# LOOKUP Demo 教学说明

本文档用于配合 `MiniLMCache` 当前的 LOOKUP v1 demo 阅读代码和观察输出。

它的目标不是解释真实 LMCache 的全部实现，而是帮助读者先建立最基础的心智模型：

- 一个请求带着 token 序列进入系统
- 系统把 token 按固定 chunk 粒度切分
- 每个完整 chunk 生成稳定 key
- metadata controller 判断哪些 chunk 已存在
- LOOKUP 只返回“从开头开始最长可复用前缀”
- 如果命中的 chunk 不在本地，则后续需要 `RETRIEVE`

---

## 1. 这个 demo 教什么

这个 demo 主要演示三件事：

1. `LOOKUP` 不是普通哈希表查询，而是“决定哪些 token 可以跳过重复 prefill”的前置判定。
2. 命中结果必须保留顺序语义。即使后面的 chunk 存在，只要前面的 chunk 缺失，也不能直接复用后面的 chunk。
3. `LOOKUP` 的输出不仅是 `hit / miss`，还会影响：
   - `MissingFrom`：引擎应从哪个 token 开始继续 prefill
   - `NeedRetrieve`：命中的 chunk 是否还要从远端取回
   - `ReservationID`：是否已经为后续 RETRIEVE 预留了最小状态

这个阶段仍然是教学型 skeleton，不涉及：

- 真实 KV 张量
- GPU 注入
- 网络传输
- tokenizer
- 生产级锁、租约、TTL、淘汰策略

---

## 2. 代码入口

建议按下面顺序阅读：

1. [cmd/minilmcache-lookup-demo/main.go](/Users/kai/PrivateProject/src/MiniLMCache/cmd/minilmcache-lookup-demo/main.go)
   - 看 demo 如何构造 `full_hit`、`partial_hit`、`first_miss`
2. [lookup/service.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/service.go)
   - 看 LOOKUP 主流程如何组织
3. [lookup/chunker.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/chunker.go)
   - 看 token 如何被切成完整 chunk
4. [lookup/keyer.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/keyer.go)
   - 看 chunk key 如何稳定生成
5. [lookup/memory/controller.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/memory/controller.go)
   - 看内存版 metadata controller 如何执行 prefix lookup
6. [lookup/service_test.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/service_test.go)
   - 看测试如何覆盖 hit / partial hit / miss / tail / reservation

---

## 3. 这版 LOOKUP 的固定规则

### 3.1 输入

- 输入只接受 `Token IDs`
- 不支持原始 prompt 字符串

### 3.2 Chunk 规则

- 只有完整 chunk 参与 lookup
- 尾部不足一个 chunk 的 token 不会被索引
- 这段尾部 token 会直接落入 missing range

例子：

```text
Tokens:    [1 2 3 4 5 6]
ChunkSize: 4

完整 chunk:
- [1 2 3 4]

尾部残块:
- [5 6]
```

在这个例子里，就算第一个 chunk 命中，最终结果也仍然会是 `partial_hit`，因为 `[5 6]` 还需要引擎自己计算。

### 3.3 命中规则

- 只接受“从第 0 个 chunk 开始连续命中”的结果
- 一旦遇到第一个 miss，本次 lookup 立即停止
- 不接受跳过前面 miss 去复用后面 chunk

这就是“最长前缀命中”的含义。

### 3.4 Retrieve Hint

- 只要任一命中 chunk 的位置不是 `local`
- `NeedRetrieve` 就会被置为 `true`

这表示：

- 当前 lookup 知道这个 chunk 可复用
- 但它不一定已经在引擎可直接使用的位置
- 后续仍然可能需要执行 retrieve

---

## 4. 核心类型怎么理解

### `Request`

LOOKUP 的输入：

- `RequestID`：用于追踪这次请求
- `EngineID`：表示是哪个引擎实例发起 lookup
- `Tokens`：本次请求的 token 序列
- `ChunkSize`：chunk 粒度

### `Chunk`

表示 token 序列中某一个完整 chunk：

- `Index`：第几个 chunk
- `Start` / `End`：它在原 token 序列里的范围
- `Key`：这个 chunk 的稳定 key

### `Hit`

表示一个命中的 chunk：

- `Chunk`：命中的 chunk 本身
- `Location`：它现在位于哪里，例如 `local` 或 `remote`

### `Result`

LOOKUP 的核心输出：

- `Status`
  - `hit`
  - `partial_hit`
  - `miss`
- `ReusablePrefixTokens`
  - 有多少前缀 token 已可复用
- `MissingFrom`
  - 从哪个 token 下标开始需要继续 prefill
- `Hits`
  - 命中的 chunk 明细
- `NeedRetrieve`
  - 是否需要后续 retrieve
- `ReservationID`
  - 为后续 retrieve 预留的最小 reservation 标识
- `Trace`
  - 用于教学和调试的过程事件

---

## 5. LOOKUP 主流程

主流程在 [lookup/service.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/service.go)。

按顺序可以概括为：

1. 校验请求是否合法
2. 计算本次 lookup 的实际 `ChunkSize`
3. 把 token 切成完整 chunk
4. 为每个 chunk 计算确定性 key
5. 交给 controller 做 prefix lookup
6. 如果开启 reservation，则为命中的结果生成一个 reservation
7. 组装 `Result`
8. 生成 `Trace`

可以把它理解成：

```text
Request
  -> chunking
  -> keying
  -> metadata lookup
  -> reservation hook
  -> result
```

---

## 6. 为什么 controller 要做 prefix lookup

内存版 controller 在 [lookup/memory/controller.go](/Users/kai/PrivateProject/src/MiniLMCache/lookup/memory/controller.go)。

它不是“遍历所有 chunk，把存在的都返回”。

它做的是：

```text
从 chunk 0 开始按顺序检查
  - 如果存在且 ready，记为 hit
  - 如果不存在或未 ready，立即停止
```

这样设计是为了保留“前缀可复用”的真实语义。

例子：

```text
chunk0: miss
chunk1: hit
chunk2: hit
```

最终结果仍然必须是：

- `Status = miss`
- `Hits = []`
- `MissingFrom = 0`

因为前缀没有建立起来，后面的命中不能直接拿来跳过 prefill。

---

## 7. 三个 demo 场景分别在说明什么

### `full_hit`

含义：

- 所有完整 chunk 都命中
- 没有尾部残块
- 整个 prefix 都可以复用

结果特征：

- `Status = hit`
- `ReusablePrefixTokens = 全部 token`
- `MissingFrom = len(tokens)`

### `partial_hit`

含义：

- 开头一部分 chunk 命中
- 后面第一个 chunk 缺失，或存在尾部残块

结果特征：

- `Status = partial_hit`
- `ReusablePrefixTokens > 0`
- `MissingFrom` 指向首个仍需计算的位置

### `first_miss`

含义：

- 第一个 chunk 就缺失
- 说明前缀完全不可复用

结果特征：

- `Status = miss`
- `ReusablePrefixTokens = 0`
- `MissingFrom = 0`

---

## 8. Trace 应该怎么看

当前输出里会带一个 `Trace`，这是教学版 demo 的重点。

典型步骤包括：

- `request_validated`
- `chunks_prepared`
- `lookup_completed`
- `hits_reserved`
- `result_built`

它的价值在于把 LOOKUP 从“黑盒返回结果”变成“可追踪的数据流”。

例如：

- 你可以看到一共切了多少个完整 chunk
- 可以看到 prefix hit 到第几个 chunk
- 可以看到 reservation 是否发生
- 可以看到最终结果是如何被拼出来的

---

## 9. 如何运行

运行 demo：

```bash
go run ./cmd/minilmcache-lookup-demo
```

运行测试：

```bash
go test ./...
```

如果你希望先从行为理解再回头读代码，建议先跑 demo，再对照源码和测试阅读。

---

## 10. 建议的课堂式阅读顺序

如果这是用于教学或分享，我建议按这个顺序讲：

1. 先讲 `LOOKUP` 的目标
   - 不是查值，而是决定哪些 token 不必重复 prefill
2. 再讲 chunk 和 chunk key
   - 为什么要固定粒度
   - 为什么 key 必须稳定
3. 再讲最长前缀命中
   - 为什么不能跳过前面的 miss
4. 再讲 `NeedRetrieve`
   - 命中不等于已经在本地可直接用
5. 最后讲 reservation stub
   - 为什么 LOOKUP 会影响后续 RETRIEVE

---

## 11. 下一步怎么演进

这份 demo 文档对应的是 LOOKUP v1。

如果继续往下扩展，最自然的顺序是：

1. 在现有 `NeedRetrieve` 和 `ReservationID` 的基础上接入 `RETRIEVE`
2. 再补一个最小化的 `STORE`
3. 最后做双实例演示，展示“一个实例产生 metadata，另一个实例 lookup + retrieve”

这样可以始终保持：

- 先把 metadata plane 讲清楚
- 再把 data plane 逐步接上

这也是当前 `MiniLMCache` 最适合教学推进的方式。
