# LMCache v0.4.4 源码详解

本文是 [LMCache v0.4.4](https://github.com/LMCache/LMCache/tree/v0.4.4) 的源码导读，聚焦核心模块、调用链和重点设计。所有源码链接都固定到 `v0.4.4` tag。

版本基线：

- PyPI：[lmcache 0.4.4](https://pypi.org/project/lmcache/)，发布于 `2026-04-23`
- GitHub tag：[v0.4.4](https://github.com/LMCache/LMCache/tree/v0.4.4)
- commit：[`6fbec463e3c047fffb4e22c97508f03b057de3bc`](https://github.com/LMCache/LMCache/tree/6fbec463e3c047fffb4e22c97508f03b057de3bc)

## 1. 源码地图

| 路径 | 作用 |
| --- | --- |
| [`lmcache/v1/cache_engine.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_engine.py) | 非 MP 路径核心 engine，负责 lookup/retrieve/store |
| [`lmcache/v1/manager.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/manager.py) | 生命周期门面，统一管理 engine、lookup、offload、API、health |
| [`lmcache/integration/vllm`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/integration/vllm) | vLLM connector 和 service factory |
| [`lmcache/v1/storage_backend`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/storage_backend) | 非 MP backend 体系：CPU、disk、remote、P2P、connector |
| [`lmcache/v1/multiprocess`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/multiprocess) | MP server、ZMQ 协议、message queue、blend server |
| [`lmcache/v1/distributed`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/distributed) | MP mode 的 L1/L2 storage manager、adapter、controller |
| [`lmcache/v1/cache_controller`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/cache_controller) | Controller / worker / registry / P2P lookup 控制面 |
| [`lmcache/v1/gpu_connector`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/gpu_connector) | GPU KV layout 与 GPU↔CPU copy 适配 |
| [`lmcache/v1/lookup_client`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/lookup_client) | scheduler 侧 lookup client/server |
| [`lmcache/v1/mp_observability`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/mp_observability) | MP mode EventBus、metrics、logging、tracing |

阅读源码时建议按两条线看：

1. 单进程/集成路径：`vLLM connector -> LMCacheManager -> LMCacheEngine -> storage_backend`。
2. MP 路径：`vLLM MP adapter -> ZMQ MessageQueueServer -> MPCacheEngine -> distributed StorageManager`。

## 2. vLLM 集成层

### 2.1 `LMCacheConnectorV1Dynamic`

入口文件：[ `lmcache/integration/vllm/lmcache_connector_v1.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/integration/vllm/lmcache_connector_v1.py)

`LMCacheConnectorV1Dynamic` 继承 vLLM 的 `KVConnectorBase_V1`，但绝大多数逻辑都委托给 `LMCacheConnectorV1Impl`。

它暴露给 vLLM 的核心方法：

- `register_kv_caches()`：worker 侧注册 vLLM paged KV tensors。
- `start_load_kv()`：forward 前启动加载。
- `wait_for_layer_load()`：layer 内等待对应层 KV 加载完成。
- `save_kv_layer()`：layer 后保存当前层 KV。
- `wait_for_save()`：forward exit 时等待保存完成。
- `get_num_new_matched_tokens()`：scheduler 侧查询外部 KV cache 命中 token 数。
- `request_finished()`：请求结束后触发异步保存或传输。

### 2.2 `LMCacheConnectorV1Impl`

入口文件：[ `lmcache/integration/vllm/vllm_v1_adapter.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/integration/vllm/vllm_v1_adapter.py)

`LMCacheConnectorV1Impl` 是 vLLM 集成的主要实现。它做四件事：

1. 读取全局 `LMCacheEngineConfig`。
2. 将 vLLM `kv_connector_extra_config` 中以 `lmcache.` 开头的配置写回 LMCache config。
3. 通过 `VllmServiceFactory` 创建 `LMCacheManager`。
4. 按 scheduler/worker role 初始化不同状态。

关键职责：

- scheduler 侧维护 unfinished requests、lookup client、request tracker。
- worker 侧维护 KV cache tensors、layerwise retrievers、save storers、blender。
- 将 vLLM 的 block allocation、request finished、forward context 等事件转换成 LMCache 的 lookup/retrieve/store。

### 2.3 `VllmServiceFactory`

入口文件：[ `lmcache/integration/vllm/vllm_service_factory.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/integration/vllm/vllm_service_factory.py)

`VllmServiceFactory` 是 engine-agnostic 架构里的 vLLM 适配器。它负责把 vLLM 配置转换成 LMCache 内部组件。

它生成的 `LMCacheMetadata` 包含：

- `model_name`
- `world_size`
- `worker_id`
- `local_worker_id`
- `kv_dtype`
- `kv_shape`
- `use_mla`
- `served_model_name`
- `chunk_size`
- engine id / extra config

这个 metadata 是后续 memory allocator、GPU connector、ObjectKey 生成和并行 rank 区分的基础。

## 3. 生命周期门面：`LMCacheManager`

入口文件：[ `lmcache/v1/manager.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/manager.py)

`LMCacheManager` 是内部组件生命周期的门面。它把 vLLM adapter 和 LMCache 内部实现解耦。

它管理的组件包括：

- `LMCacheEngine`
- `LookupClient` / `LookupServer`
- `ZMQOffloadServer`
- `RuntimePluginLauncher`
- `InternalAPIServer`
- `HealthMonitor`

关键流程：

```text
LMCacheConnectorV1Impl
  -> VllmServiceFactory(...)
  -> LMCacheManager(...)
      -> get_or_create_metadata()
      -> get_or_create_lmcache_engine()
      -> maybe_create_lookup_client()
      -> maybe_create_lookup_server()
      -> maybe_create_offload_server()
      -> maybe_create_runtime_plugin_launcher()
      -> maybe_create_internal_api_server()
```

`post_init()` 在 KV caches 注册后执行，原因是很多 GPU connector 和 allocator 需要真实 KV tensor 才能完成初始化。初始化失败时，manager 会标记 degraded mode，让系统回退到 recompute。

## 4. 核心引擎：`LMCacheEngine`

入口文件：[ `lmcache/v1/cache_engine.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_engine.py)

`LMCacheEngine` 是非 MP 路径最重要的类。它负责把 token 序列变成 cache key，把 GPU KV 搬到 storage backend，或从 backend 搬回 GPU。

### 4.1 初始化

构造参数包括：

- `LMCacheEngineConfig`
- `LMCacheMetadata`
- `TokenDatabase`
- `GPUConnectorInterface`
- TP group 的 broadcast 函数

初始化时会设置：

- controller worker
- async loading
- event manager
- layerwise 模式
- KV events
- PD receiver 下的 `remove_after_retrieve`
- store/retrieve backend location
- stats monitor
- pin monitor
- health monitor hook

`post_init()` 之后才真正创建 `StorageManager`，因为这一步需要 KV shape、GPU connector 和部分 runtime 信息。

### 4.2 `lookup()`

`lookup()` 判断 tokens/hashes 对应的 KV chunk 是否已经存在。

流程：

1. 健康检查，失败则返回 0。
2. 调用 `token_database.process_tokens()` 生成 `(start, end, key)`。
3. 调用 `storage_manager.batched_contains()` 查询 backend。
4. 只要遇到第一个 miss，就停止，返回连续 prefix token 数。
5. 如果 `pin=True`，记录 lookup id 与 pinned keys，后续 retrieve 可直接使用对应 location。

重点：

- 返回的是 token 数，不是 chunk 数。
- 命中必须连续。
- layerwise 模式会把一个 logical key 拆成所有 layer 的 key，只有所有 layer 命中才算该 chunk 命中。

### 4.3 `retrieve()`

`retrieve()` 从 storage backend 取回 KV，并写入 vLLM 的 GPU paged KV buffer。

流程：

1. 健康检查。
2. 处理 token chunk。
3. 同步或异步调用 `_process_tokens_internal()` / `_async_process_tokens_internal()`。
4. 从 backend 获取 `MemoryObj`。
5. 如启用 `save_only_first_rank`，第一 rank broadcast memory object 或 metadata。
6. 调用 `gpu_connector.batched_to_gpu()`。
7. 更新返回 mask，释放引用。

关键细节：

- `ret_mask` 标记哪些 token 已由 LMCache retrieve。
- 如果中间 chunk retrieve 失败，后续 chunk 即使命中也会被截断，保证 prefix 语义。
- `async_loading` 用于让加载和 forward/layer 执行更好地重叠。

### 4.4 `store()`

`store()` 将新生成的 KV cache 写入 LMCache。

流程：

1. 健康检查与 freeze mode 检查。
2. 根据 tokens/hashes/mask 计算要存储的 token 数。
3. `TokenDatabase.process_tokens()` 生成 chunk key。
4. 为每个 chunk 根据 metadata 计算 KV shape。
5. GPU connector 从 vLLM paged KV buffer 取出对应 KV。
6. 生成 `MemoryObj`。
7. storage manager 异步提交 put/store task。

重点：

- mask 的 False 必须在前缀，且 false token 数需要和 chunk 对齐。
- 支持保存不完整 chunk 的配置。
- 后台存储让响应主路径不等待慢后端。

### 4.5 `LMCacheEngineBuilder`

同一 engine instance 通过 builder 单例化管理，避免同一进程中重复创建 engine 和底层资源。

## 5. 非 MP Storage Backend

入口目录：[ `lmcache/v1/storage_backend` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/storage_backend)

非 MP 路径的 `StorageManager` 在 [ `storage_backend/storage_manager.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/storage_manager.py)。它管理多个 backend，并维护一个独立 asyncio event loop。

主要职责：

- 创建 backends。
- 选择 allocator backend。
- 管理 `LocalCPUBackend` 热缓存。
- 对 backend 进行 batched contains/get/put。
- 支持 freeze mode、bypass mode、hot cache 开关。
- 处理 async lookup server 和 async serializer。

重要 backend：

- [`local_cpu_backend.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/local_cpu_backend.py)：CPU 热层和 allocator backend。
- [`local_disk_backend.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/local_disk_backend.py)：本地磁盘后端。
- [`remote_backend.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/remote_backend.py)：remote connector wrapper。
- [`p2p_backend.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/p2p_backend.py)：P2P cache sharing。
- [`connector`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/storage_backend/connector)：Redis、Valkey、S3、Mooncake、InfiniStore、external connector 等。

非 MP backend 的风格更接近 engine 内嵌缓存层，MP backend 则更像独立 cache server。

## 6. P2P Backend

入口文件：[ `lmcache/v1/storage_backend/p2p_backend.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/storage_backend/p2p_backend.py)

`P2PBackend` 是非中心化 KV 共享的关键实现。

核心字段：

- `peer_init_url`：当前 worker 的 peer init endpoint。
- `peer_lookup_url`：当前 worker 接收 peer lookup/get 请求的 endpoint。
- `local_lookup_cache`：本地 lookup cache，v0.4.4 里仍有 TODO。
- `target_peer_info_mapping`：目标 peer 的 lookup socket 和 URL。
- `lookup_id_to_peer_mapping`：一次 lookup 与目标 peer 的绑定。
- `transfer_channel`：NIXL 或 mock/socket transfer channel。
- `local_cpu_backend`：真实 KV objects 的本地来源或目标。

### 6.1 lookup 阶段

`batched_async_contains()`：

1. 将 `CacheEngineKey` 转为 chunk hashes。
2. 发送 `BatchedP2PLookupMsg` 给 controller。
3. controller 返回包含 target peer 的 layout info。
4. backend lazy init peer connection。
5. 记录 `lookup_id -> peer`。
6. 返回命中 chunk 数。

### 6.2 get 阶段

`batched_get_non_blocking()`：

1. 根据 lookup id 找到 target peer。
2. 为目标 chunks 在本地分配 `MemoryObj`。
3. 通过 transfer channel 获取 local memory indexes。
4. 向 source peer 发送 `BatchedLookupAndGetMsg`。
5. source peer 从自己的 `LocalCPUBackend` 读取 objects。
6. source peer 使用 `async_batched_write()` 将 bytes 写入 target buffers。
7. target peer pin 命中的 objects，释放 miss objects。

这个路径把 controller 留在元数据面，真实 KV bytes 不经过 controller。

## 7. Multiprocess Server

入口目录：[ `lmcache/v1/multiprocess` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/multiprocess)

MP mode 将 cache server 从推理进程中拆出来，vLLM 通过 ZMQ 与它通信。

### 7.1 `MessageQueueServer`

入口文件：[ `lmcache/v1/multiprocess/mq.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/multiprocess/mq.py)

`MessageQueueServer` 使用 ZMQ DEALER/ROUTER 模式接收请求，按 request type 派发到 handler。

重点：

- SYNC handler 在 ZMQ 主循环快速执行。
- BLOCKING handler 交给线程池，适合 GPU copy 或 I/O。
- affinity pool 保证同一 vLLM instance 的 GPU 操作落到固定 worker thread，降低锁竞争。

### 7.2 `MPCacheEngine`

入口文件：[ `lmcache/v1/multiprocess/server.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/multiprocess/server.py)

`MPCacheEngine` 是 MP server 的核心业务类。它持有：

- `StorageManager`
- `SessionManager`
- `TokenHasher`
- GPU context / registered KV caches
- observability hooks

它的 RPC 语义大致对应：

- `REGISTER_KV_CACHE`：注册 GPU KV cache tensors。
- `STORE`：从 GPU 复制 KV 到 L1。
- `LOOKUP`：执行 prefix lookup 并提交 prefetch。
- `RETRIEVE`：从 L1 读取 prefetched objects 并复制回 GPU。
- `FREE_LOOKUP_LOCKS`：取消请求时释放 lookup 持有的 read locks。
- `END_SESSION`：清理 request/session 状态。
- `CLEAR`：清空缓存。

### 7.3 Protocol definitions

入口目录：[ `lmcache/v1/multiprocess/protocols` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/multiprocess/protocols)

`protocols/base.py` 定义：

- `RequestType`
- `HandlerType`
- `ProtocolDefinition`

其他文件按功能拆分 protocol：

- `engine.py`
- `controller.py`
- `debug.py`
- `observability.py`
- `blend.py`
- `blend_v2.py`

新增 request type 时，需要同时补 enum、protocol definition、engine handler、server 注册。

## 8. MP Distributed Storage

入口目录：[ `lmcache/v1/distributed` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/distributed)

MP storage manager 是 v0.4.x 的核心变化之一。

### 8.1 `StorageManager`

入口文件：[ `lmcache/v1/distributed/storage_manager.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_manager.py)

它组合：

- `L1Manager`
- `L1EvictionController`
- L2 adapters
- `StoreController`
- `PrefetchController`

核心 API：

- `reserve_write(keys, layout_desc, mode)`：两阶段写入第一步。
- `finish_write(keys)`：写入完成并触发 store controller。
- `submit_prefetch_task(keys, layout_desc)`：先查 L1，miss 部分提交 L2 prefetch。
- `query_prefetch_status(handle)`：查询 L2 load 是否完成。
- `read_prefetched_results(keys)`：读取 L1 objects，并保证异常时释放 read lock。
- `finish_read_prefetched(keys)`：释放 read locks。
- `clear()` / `close()` / `report_status()`。

### 8.2 `ObjectKey` 和 layout

入口文件：[ `lmcache/v1/distributed/api.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/api.py)

`ObjectKey` 是 MP mode 的对象 ID。它由 `chunk_hash`、`model_name`、`kv_rank` 组成。`kv_rank` 当前用 packed integer 编码 world/rank/local rank 信息，用来区分并行布局。

`MemoryLayoutDesc` 描述一个 object 的 shape 和 dtype 列表，L2 load 时用它为 L1 分配正确 buffer。

`PrefetchHandle` 封装 prefetch request id、L1 prefix hit count、总 key 数和 submit time。

### 8.3 `L1Manager`

入口文件：[ `lmcache/v1/distributed/l1_manager.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l1_manager.py)

`L1Manager` 是 CPU memory 中 object 状态机：

```text
None
  -> reserve_write()
write_locked
  -> finish_write()
ready
  -> reserve_read()
read_locked
  -> finish_read()
ready
```

重点实现：

- 全局 lock 保证批量 key 操作原子性。
- 每个 object 用 `TTLLock` 管 read/write。
- listeners 接收 write/read/delete/access 事件。
- memory allocation 委托给 `L1MemoryManager`。
- OTel gauge 上报 L1 memory 使用。

### 8.4 `StoreController`

入口文件：[ `lmcache/v1/distributed/storage_controllers/store_controller.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_controllers/store_controller.py)

它注册 `StoreListener` 到 `L1Manager`。当 `finish_write()` 发生时，listener 只快速入队并 signal eventfd，慢 I/O 由 controller 后台线程处理。

它的循环做三件事：

1. 从 listener eventfd 取新 keys。
2. 按 `StorePolicy` 决定目标 L2 adapters。
3. 对每个 adapter 提交 `submit_store_task()`，完成后释放 L1 read locks，并按策略删除 L1 keys。

### 8.5 `PrefetchController`

入口文件：[ `lmcache/v1/distributed/storage_controllers/prefetch_controller.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_controllers/prefetch_controller.py)

它处理 L2 lookup 和 load：

1. 外部提交 prefetch request。
2. 后台线程向所有 L2 adapters 提交 lookup-and-lock。
3. 收集 bitmap 结果。
4. 用 `PrefetchPolicy` 生成 load plan。
5. trim 到最长连续 prefix。
6. reserve L1 write buffers。
7. 提交 adapter load。
8. load 完成后将 L1 对象转成 read-locked。
9. query 接口返回命中 prefix chunk 数。

### 8.6 `EvictionController`

入口文件：[ `lmcache/v1/distributed/storage_controllers/eviction_controller.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_controllers/eviction_controller.py)

L1 eviction 根据 watermark 周期触发。它需要和 L1 locks 配合，只淘汰安全对象。L2 eviction 在 v0.4.4 也开始具备统一控制结构，用于支持带容量的 adapter。

## 9. L2 Adapter 框架

入口目录：[ `lmcache/v1/distributed/l2_adapters` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/distributed/l2_adapters)

### 9.1 `L2AdapterInterface`

入口文件：[ `lmcache/v1/distributed/l2_adapters/base.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/base.py)

接口分三组：

- Store：`submit_store_task()`、`pop_completed_store_tasks()`。
- Lookup and lock：`submit_lookup_and_lock_task()`、`query_lookup_and_lock_result()`、`submit_unlock()`。
- Load：`submit_load_task()`、`query_load_result()`。

结果使用 `Bitmap` 表示每个 key 的成功/失败。adapter 不负责 `MemoryObj` 生命周期；buffer 由 caller 分配和释放。

每个 adapter 必须提供三个不同 event fd：

- store event fd
- lookup event fd
- load event fd

这让 controller 能用 `select.poll()` 统一等待多个后端。

### 9.2 Adapter 类型

v0.4.4 主要 adapter：

- [`nixl_store_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/nixl_store_l2_adapter.py)：NIXL store，支持 POSIX/GDS/GDS_MT/HF3FS/OBJ。
- [`nixl_store_dynamic_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/nixl_store_dynamic_l2_adapter.py)：动态 NIXL store，支持 persist/recover。
- [`fs_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/fs_l2_adapter.py)：纯文件系统 adapter。
- [`mooncake_store_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/mooncake_store_l2_adapter.py)：Mooncake Store native connector。
- [`mock_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/mock_l2_adapter.py)：测试和 benchmark adapter。
- [`plugin_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/plugin_l2_adapter.py)：Python plugin adapter。
- [`native_connector_l2_adapter.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/l2_adapters/native_connector_l2_adapter.py)：native connector wrapper。

### 9.3 Policy

Store policy：[ `storage_controllers/store_policy.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_controllers/store_policy.py)

Prefetch policy：[ `storage_controllers/prefetch_policy.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/distributed/storage_controllers/prefetch_policy.py)

默认行为：

- store：写入所有 adapters，保留 L1。
- skip_l1：写入 L2 后删除 L1，适合 buffer-only mode。
- prefetch default：多 adapter 命中时选第一个 adapter。
- retain：与 default 类似，但 prefetched keys 留在 L1。

## 10. Cache Controller

入口目录：[ `lmcache/v1/cache_controller` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/cache_controller)

Controller 是跨 worker、跨实例的控制平面。

### 10.1 `LMCacheControllerManager`

入口文件：[ `lmcache/v1/cache_controller/controller_manager.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_controller/controller_manager.py)

它创建：

- controller pull socket：接收 worker push 消息。
- controller reply socket：处理 worker/controller req-reply。
- dedicated heartbeat socket：避免 heartbeat 被其他请求阻塞。
- `RegistrationController`
- `KVController`
- `LMCacheClusterExecutor`

它负责分发四类消息：

- worker fire-and-forget message
- worker request message
- orchestration message
- heartbeat

### 10.2 `RegistrationController`

入口文件：[ `lmcache/v1/cache_controller/controllers/registration_controller.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_controller/controllers/registration_controller.py)

职责：

- 注册 instance-worker 到 registry tree。
- 保存 worker socket、ip、port、peer init URL。
- 处理 deregister。
- 更新 heartbeat。
- 在 worker 未注册或 controller 重启后触发 full sync command。
- 查询 worker info。

### 10.3 `KVController`

入口文件：[ `lmcache/v1/cache_controller/controllers/kv_controller.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_controller/controllers/kv_controller.py)

职责：

- 维护 worker 持有的 chunk hash registry。
- 处理 batched admit/evict。
- 执行 lookup、clear、pin、compress、decompress、move、check_finish。
- 处理 P2P lookup。
- 管理 full sync 状态。

它使用 `(instance_id, worker_id) -> location -> set(chunk_hash)` 的结构做 KV pool。源码注释指出，在 instance 数量小且稳定时 lookup 复杂度可接受；实例规模很大时需要更高效的 reverse index controller。

### 10.4 `LMCacheWorker`

入口文件：[ `lmcache/v1/cache_controller/worker.py` ](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/cache_controller/worker.py)

worker 运行在 LMCache-enabled serving instance 内，负责：

- 向 controller 注册。
- 启动 worker reply socket。
- 发送 KV operation。
- 执行 controller 下发的 clear/pin/move/compress/decompress。
- 发送 heartbeat。
- 根据 heartbeat command 做 full sync。

## 11. GPU Connector 和 Memory

入口目录：

- [`lmcache/v1/gpu_connector`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/gpu_connector)
- [`lmcache/v1/memory_management.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/memory_management.py)

GPU connector 的职责是理解 serving engine 的 KV layout。对 vLLM 来说，它需要从 paged KV cache 中按 slot mapping 抽取 chunk，也要把 retrieve 出来的 chunk 写回正确位置。

Memory 模块提供：

- `MemoryObj`
- `TensorMemoryObj`
- `MemoryObjMetadata`
- `MemoryAllocatorInterface`
- `PagedTensorMemoryAllocator`
- `MixedMemoryAllocator`
- `CuFileMemoryAllocator`

这些对象把 KV cache 从裸 tensor 变成可引用、可 pin、可释放、可跨 backend 传递的资源。

## 12. 观测和健康

入口目录：[ `lmcache/v1/mp_observability` ](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/mp_observability)

核心类：

- [`event_bus.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/mp_observability/event_bus.py)：bounded queue + subscriber dispatch。
- [`event.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/mp_observability/event.py)：event type 和 event payload。
- [`otel_init.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/mp_observability/otel_init.py)：OpenTelemetry provider 初始化。
- [`subscribers`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/mp_observability/subscribers)：metrics、logging、tracing subscriber。

Health 相关：

- [`lmcache/v1/health_monitor`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/health_monitor)
- [`lmcache/v1/health_monitor/checks/remote_backend_check.py`](https://github.com/LMCache/LMCache/blob/v0.4.4/lmcache/v1/health_monitor/checks/remote_backend_check.py)

健康检查失败时，engine 侧会跳过 LMCache 操作，保留推理服务可用性。

## 13. 典型调用链

### 13.1 非 MP lookup/retrieve/store

```text
vLLM scheduler
  -> LMCacheConnectorV1Dynamic.get_num_new_matched_tokens()
  -> LMCacheConnectorV1Impl.get_num_new_matched_tokens()
  -> LMCacheManager.lookup_client or LMCacheEngine.lookup()
  -> TokenDatabase.process_tokens()
  -> StorageManager.batched_contains()

vLLM worker forward
  -> start_load_kv()
  -> LMCacheEngine.retrieve()
  -> StorageManager.batched_get()
  -> GPUConnector.batched_to_gpu()

request finished / layer save
  -> save_kv_layer() / request_finished()
  -> LMCacheEngine.store()
  -> GPUConnector extract KV
  -> StorageManager.batched_submit_put_task()
```

### 13.2 MP lookup/store/retrieve

```text
vLLM MP connector
  -> ZMQ request
  -> MessageQueueServer
  -> MPCacheEngine
  -> TokenHasher / SessionManager
  -> distributed.StorageManager
      -> L1Manager
      -> PrefetchController / StoreController
      -> L2AdapterInterface
```

### 13.3 P2P sharing

```text
target worker
  -> P2PBackend.batched_async_contains()
  -> controller BatchedP2PLookupMsg
  -> KVController.batched_p2p_lookup()
  -> target P2PBackend._ensure_peer_connection()
  -> target P2PBackend.batched_get_non_blocking()
  -> source P2PBackend._handle_batched_lookup_and_get()
  -> transfer_channel.async_batched_write()
```

## 14. 扩展点

### 14.1 新增非 MP remote connector

关注：

- [`storage_backend/connector`](https://github.com/LMCache/LMCache/tree/v0.4.4/lmcache/v1/storage_backend/connector)
- connector adapter 的 get/put/remove/batched 方法
- `RemoteBackend` 的包装逻辑

适用于接入新的 Redis-like、object store 或外部服务。

### 14.2 新增 MP L2 adapter

关注：

- `L2AdapterConfigBase`
- `register_l2_adapter_type()`
- `L2AdapterInterface`
- `create_l2_adapter()` factory
- event fd 和 task result 语义

实现时最容易出错的是 event fd 不唯一、lookup result 被重复读取、load buffer 生命周期和 L1 lock 释放。

### 14.3 新增 MP request type

需要改：

- `multiprocess/protocols/base.py`
- 对应 `protocols/*.py`
- `MPCacheEngine` handler
- `run_cache_server()` handler 注册
- 必要时补 vLLM connector 或 client 调用

### 14.4 新增 observability subscriber

需要改：

- `mp_observability/event_bus.py` 的 subscriber 基类实现
- `mp_observability/subscribers/*`
- `mp_observability/config.py` 中 `init_observability()`

## 15. 阅读建议

如果只想快速理解 LMCache：

1. 先读 `README.md` 和 `docs/source/developer_guide/architecture.rst`。
2. 再读 `cache_engine.py` 的 `lookup()`、`retrieve()`、`store()`。
3. 然后读 vLLM connector 的 `LMCacheConnectorV1Dynamic` 和 `LMCacheConnectorV1Impl`。
4. 如果关注分布式，直接读 `docs/source/mp/architecture.rst` 和 `distributed/storage_manager.py`。
5. 如果关注 P2P，读 `p2p_backend.py`、`cache_controller/controllers/kv_controller.py`、`worker.py`。

如果要改代码：

- 改 vLLM 生命周期：先从 `integration/vllm` 入手。
- 改 KV storage 行为：非 MP 看 `storage_backend`，MP 看 `distributed`。
- 改跨实例控制：看 `cache_controller`。
- 改观测：看 `mp_observability`。
- 改 GPU copy/layout：看 `gpu_connector` 和 `memory_management.py`。

## 16. 参考资料

- [PyPI: lmcache 0.4.4](https://pypi.org/project/lmcache/)
- [GitHub: LMCache v0.4.4](https://github.com/LMCache/LMCache/tree/v0.4.4)
- [Architecture Overview](https://github.com/LMCache/LMCache/blob/v0.4.4/docs/source/developer_guide/architecture.rst)
- [MP Architecture](https://github.com/LMCache/LMCache/blob/v0.4.4/docs/source/mp/architecture.rst)
- [MP L2 Storage](https://github.com/LMCache/LMCache/blob/v0.4.4/docs/source/mp/l2_storage.rst)
- [P2P KV Cache Sharing](https://github.com/LMCache/LMCache/blob/v0.4.4/docs/source/kv_cache/p2p_sharing.rst)
