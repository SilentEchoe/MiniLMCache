# AGENT.md

本文件面向在 `MiniLMCache` 仓库中工作的 agent / 协作者，用于快速理解项目目标、架构边界和实现约束。

## 项目定位

`MiniLMCache` 是一个**最小化的 Go 教学项目**，用于演示类似 LMCache 的 LLM KV Cache 共享机制。

它关注的是以下核心问题：

- KV cache 如何被切分为 chunk
- chunk 如何被稳定地标识和索引
- 元数据如何与真实缓存数据分离
- 一个实例如何发现另一个实例已经生成的缓存
- 远端缓存如何被拉取并复用

它**不是**生产级缓存系统，也**不会**运行真实模型推理。

## 项目目标

实现或扩展本项目时，应优先满足以下目标：

- `Educational`：代码应该容易阅读、容易追踪
- `Minimal`：只保留核心机制，不引入过重实现
- `Observable`：lookup、miss、store、pull、reuse 等过程应可见
- `Extensible`：后续可继续演化出压缩、多级缓存、淘汰策略或 P2P 传输

## 明确非目标

除非任务明确要求，否则不要把项目往以下方向扩展：

- 真实 tensor 存储
- GPU 显存管理
- vLLM 集成
- 高性能网络传输
- 生产级一致性保证
- 完整的缓存淘汰与生命周期策略
- 多租户安全隔离

如果某项改动明显把项目从“概念演示”推进到“生产系统”，应先收敛范围，再实现。

## 核心流程

MiniLMCache 应始终围绕下面这条主链路组织：

1. 请求到达某个 inference instance
2. 实例根据 prompt 或 token block 计算确定性的 cache key / chunk key
3. 实例向 metadata service 查询所需 chunk 是否已存在
4. 若存在，则从 remote storage 拉取并复用
5. 若不存在，则模拟 prefill 并生成新 chunk
6. 将生成的 chunk 写入 cache backend
7. 更新 metadata，使其他实例后续可以发现这些 chunk

任何实现都不应打乱这条链路的可读性。

## 架构边界

项目需要明确区分两层：

- `Metadata plane`：负责 chunk 注册、发现、归属信息、位置映射
- `Data plane`：负责 chunk 字节数据的实际存储与传输

这层分离是本项目最重要的设计点之一。新增代码时，不要把 metadata 逻辑和 chunk 数据存取逻辑耦合成一个黑盒。

## 组件职责

基于 README，系统中的角色应大致保持为：

- `Engine`：模拟推理实例，负责 lookup、generate、push、pull、reuse
- `Controller`：维护 chunk metadata、ownership、location mapping
- `Cache Store`：保存序列化后的 chunk 数据，并提供 store / fetch 能力

如果后续补代码，建议优先保持职责单一，而不是把所有逻辑堆进一个包或一个主流程函数。

## 实现建议

当前仓库是极简状态。后续实现时，优先遵守以下原则：

- 优先使用 Go 标准库和简单抽象
- 先做可运行、可解释的 demo，再考虑复杂优化
- 命名要贴近缓存复用语义，避免引入与真实推理框架强绑定的概念
- 使用日志、事件输出或小型 trace 显式展示 hit / miss / pull / store 路径
- 保持行为可重复、可测试，优先使用确定性输入和稳定 key 生成方式
- 若引入远端存储或控制器接口，先提供内存版或 mock 版实现

## 建议的演进方向

如果需要从零开始补实现，可以按下面的顺序推进：

1. 定义 chunk、cache key、metadata 的基础数据结构
2. 实现一个内存版 metadata controller
3. 实现一个内存版 cache store
4. 实现 engine 的 lookup / generate / store / pull / reuse 主流程
5. 增加一个双实例演示，展示跨实例缓存复用
6. 为 hit、miss、partial hit、remote pull 等场景补测试

## 文档维护要求

如果后续实现与本文件中的边界或术语发生变化，应同步更新：

- `README.md`
- `AGENT.md`

文档应始终反映“这是一个教学型、最小化、可观察的 KV cache 共享演示项目”这一核心定位。

## 当前仓库状态

当前仓库已经包含 LOOKUP v1 的 Go 基础实现：

- `go.mod`
- `lookup/`：LOOKUP 的核心类型、chunking、keying、service、trace
- `lookup/memory/`：内存版 metadata controller 与 reservation stub
- `cmd/minilmcache-lookup-demo/`：CLI demo
- `skills/minilmcache-code/`：仓库内版本化的 MiniLMCache 专用 Codex skill 副本

当前实现边界：

- 只实现 LOOKUP 的 metadata-plane 判定
- 输入只接受 token IDs
- 只复用从第 0 个 chunk 开始的最长连续命中前缀
- 尾部不足一个 chunk 的 token 不参与 lookup，而是进入 missing range
- reservation 仅为后续 RETRIEVE 对接预留最小骨架

因此后续 agent 在继续扩展时，应优先围绕 LOOKUP -> RETRIEVE -> STORE 的顺序推进，而不是跳到真实 tensor、GPU 或生产级分布式细节。
