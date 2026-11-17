# 架构与控制协议

当前架构由共享 NSGo 控制端、Go 被控端和可替换的 Go/Cloudflare Worker 设备服务组成。各层边界、数据所有权和并发约束以 [后端架构规范](engineering/architecture.md) 为准，开发入口见 [工程知识库](engineering/README.md)。

- 桌面、移动端和 Web 共用 `clients/nsgo/hubui`；原生端直接调用 Go 核心，Web 使用 Go WASM + DOM 和浏览器适配层。
- 原生与 Web 构建入口统一为根目录 `scripts/build.sh`，默认产物在根目录 `dist/`，平台源码在 `clients/nsgo/native` 和 `clients/nsgo/nativecap`。
- 服务维护同一份 `/p2p/*` 契约的两套实现；Go 使用 SQLite，Worker 使用 D1/Durable Objects。具体行为见 [双服务一致性](engineering/service-parity.md)。
- 远程调用、授权租约、帧格式和渐进消息同步见 [当前协议](platform/protocol.md)；Provider 接口与项目归属见 [Agent 适配](platform/agent-providers.md)。
- 已删除的 UI 原型、旧 HTTP/relay 路由说明和 C ABI 构建不再作为开发或验证入口。

## Codex backend

### 平台适配

`internal/system` 用 Go 文件后缀和 build tags 在编译时选择实现：`*_darwin.go` 保留 macOS 的安装、进程、启动和连接检测；`*_windows.go` 查询当前用户的 `OpenAI.Codex` 应用包、解析清单入口，并用 Windows 原生进程 API 匹配完整路径和主进程。其它系统使用显式的兜底实现，不通过 `runtime.GOOS` 引入其它平台实现，也不打包 PowerShell 或新的运行时依赖。

Windows 安装查询有 4 秒超时、隐藏控制台窗口，并短期缓存包位置；每次刷新仍校验文件和运行进程。App 接入使用 Windows 命名管道，Unix 使用当前用户的本地 socket。平台构建通过不等于真实 Windows App 端到端验证通过。

### 生命周期约束

当前 NSGo 桌面直接通过 `internal/bridge.CallContext` 调用同一进程的 Go 应用服务，不使用 C ABI、helper 或本机 HTTP 调用自身。关闭窗口仅隐藏，托盘可恢复窗口；明确退出调用 Go 清理逻辑，取消请求、关闭连接并保存 SQLite。生命周期与取消验证见 `internal/app/embedded_test.go` 和 `internal/bridge/context_test.go`。

Codex App 接入改用本地 owner/follower IPC，不改变 App 启动参数、不重启 App，也不设置 `CODEX_APP_SERVER_WS_URL`。App 已有会话由原 owner 执行，ReignHub 通过 IPC 跟随状态并转交发消息、追加、停止和审批。初始 snapshot 与后续 Immer 数组路径 patch 严格校验来源、会话 ID、方法版本及 base revision；缺口重新跟随，不继续应用错误状态。当前读取已验证的状态 schema v11/v13，未知版本明确失败；ReignHub 自己发布的 owner 状态仍为 v11，不能据此声称完整支持所有 v13 owner 行为。初始历史与 watch 独立，订阅失败时仍可只读回退，不为读取调用 `resume`。会话创建来源、当前 owner 和 schema 版本分别判断。

ReignHub 另启自己拥有的私有 stdio app-server，用于目录/历史读取及远程新建会话。它作为这些会话的 owner 发布状态，原 App 可作为 follower 操作。读取和订阅不 `resume`；写入先发现 owner，仅在没有 owner 且操作系统写锁空闲时才恢复原 ID。锁探测不是原子接管，最终以 Codex 获取写锁的结果为准。关闭 ReignHub 时关闭私有执行器及 IPC，不终止原 App。远程创建的运行中任务可能因此中断；原 App 后续能否恢复仍由其自身逻辑和当时状态决定。

请求身份在 IPC owner 侧去重。同一个消息 ID 换正文会被拒绝；丢失 ACK 或超时属于结果待确认，不自动换后端重发。排队消息保留文本及支持的附件，不支持的结构明确报错。未确认的排队消息停止自动发送，用户移除后可以继续处理其他消息；同一未确认 ID 不会再次执行。审批先认领再答复，避免多端重复处理。

当前打开的会话使用通用 Provider 实时订阅接口：原生消息文本增量、公开摘要和工具状态规范化后经加密连接传送。事件按会话分别保留（每会话最多 1024 条/2 MiB，全局 8 MiB/128 会话），大量输出优先淘汰自身记录。游标只有在该会话丢失事件或 epoch 变化时才要求重新建立基线。正文和已应用游标在同一缓存快照中保存，未完成历史不得提交恢复游标。

历史默认最近 30 turns，正文按 2 KiB 渐进发送；更早历史按页请求，IPC 完整历史具有 24 MiB 总预算及重复游标防护。私有 owner 的发布采用 4 个有界并发任务，单会话失败独立退避，不阻塞其他会话。控制端历史读取使用 30 秒预算，网络失败短退避、忙碌/写锁中退避、结构或超限错误长退避；重新进入会话或连接恢复会重新探测。此策略只用于幂等读取，不能用于重放写操作。原始推理、工具输出和原始协议通知不向通用事件接口透传。

验证范围与限制见 [IPC 实现验收记录](research/codex-ipc-implementation-2026-11-17.md)。隔离协议与进程测试不代替真实 App、多窗口和 Windows 端到端验证。
