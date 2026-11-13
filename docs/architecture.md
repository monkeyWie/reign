# Provider 与控制协议

本文记录现有 Go/MyGo + Flutter 私有原型协议。最新产品、Flutter + shadcn_flutter / Go 分层、双角色桌面、设备授权与数据库同步以 [产品 v2 基准](product-v2.md) 为准，新 MVP 实现另见 [MVP 协议](mvp.md)。本文的 revision 快照缓存不是目标持久化增量日志。

开源版提供无账号的完整自部署；闭源 SaaS 组合相同连接核心，在外层处理匿名归属、账号、租户和计费。连接核心只处理经验证的 namespace 与设备连接权限，不依赖 SaaS 数据模型。目标边界见 [开源核心与闭源 SaaS 边界](open-source-boundary.md)；当前代码尚未完成此模块提取。

`internal/provider.Provider` 是 Agent 集成边界。第三方 Agent 的进程、原生 API 或 CLI 细节留在 Provider 内；网络客户端只使用通用会话、turn、item 和 approval 数据。

## 本机 API

默认 `http://127.0.0.1:7331/rpc`，只接受带 `Authorization: Bearer <access-key>` 的 POST。拒绝浏览器 Origin，限制请求为 1 MiB。与远程共用同一个 dispatcher。此接口没有 HTML 页面。

```json
{"id":"r1","method":"providers"}
```

```json
{"id":"r2","provider":"codex","method":"transcript","session":"original-thread-id"}
```

```json
{"id":"r3","provider":"codex","method":"send","session":"original-thread-id","text":"继续完成实现"}
```

成功返回 `{"id":"r3","result":null}`，失败返回 `{"id":"r3","error":"..."}`。只读快照还返回 SHA-256 `revision`；客户端携带同一 revision 再次查询时，主机重新检查真实内容，相同则返回 `notModified: true` 并省略 result。远程客户端以自己保存的快照还原结果，避免 relay 重复传输未变化的历史。写操作不使用此机制。方法见 `internal/control/control.go`：

| Method | 额外参数 | 返回 |
|---|---|---|
| providers | 无 | Provider Status 列表，含 installed / running / installPath |
| network | 无 | 中继状态和已认证的在线客户端链路 |
| sessions | provider、cursor（可选） | SessionPage |
| transcript | provider、session、cursor（可选） | Transcript，turn 按时间正序 |
| create | provider、options.cwd、options.model（可选） | Session |
| send | provider、session、text | null |
| steer | provider、session、turn、text | null |
| interrupt | provider、session、turn | null |
| approvals | provider | Approval 列表 |
| decide | provider、approval、decision | null |
| connectDesktop | provider、expected、confirmRestart | null |

`expected` 是确认界面中展示的 App 实例数组（PID、启动时间、路径）。后台重新发现后要求它们完全一致，避免误作用于刚重新启动的其它实例。主机发现请求是被动的 `ps` 读取，无 Accessibility 权限依赖。

以上 HTTP API 是前代独立服务的协议参考。当前桌面本机调用直接走进程内 C FFI，不创建该 HTTP listener，也不使用旧的 `remoteEnabled` / `remoteListen` 配置。远程连接通过设备服务配对，再走 WebRTC 或加密 relay。

## Relay 与 WebRTC

`/relay?role=host&id=<host-id>` 或 `role=client`，原生 HTTP Bearer 使用 relay token；浏览器客户端使用子协议 `agenthub.v1, auth.<base64url-token>`，服务端只协商前者。验证精确 Origin，浏览器不能声明 host 角色。当前共享 token 仅适合私有实例，公有租户授权见 `product-and-tenancy.md`。relay 给客户端分配随机 peer ID，客户端不能指定它；主机给某个 peer 回复，客户端只能给所属主机发送。

外层 Frame：`{peer, kind, body}`，`body` 是 base64 编码的加密字节。`welcome` 提供 peer ID，`bye` 通知断开。`hello` 是加密配对握手，`signal` 是加密 SDP，`chunk` 是加密控制协议分片。relay 不解析其中的业务 JSON。

握手首先在主机配对密钥派生的 AES-256-GCM 密钥下交换两端各自生成的 256 位随机值，握手包使用随机 96 位 nonce，主机答复须回显客户端随机值。数据密钥再绑定双方随机值与 peer ID 派生；即使 relay 重用旧 peer ID 或重放旧握手，新会话也不会复用原数据密钥。发送方向使用不同 nonce 前缀和单调计数器，AAD 包含协议版本、peer、方向和消息类型。接收端采用 4096 包滑动窗口，允许通道切换造成的有限乱序并拒绝旧包/重复包。

RPC JSON 以最多 12 KB 的明文片段传输，最多重组 16 MiB；WebRTC 与 relay 使用相同分片格式。DataChannel 名称 `agenthub-v1`，可靠、有序。仅保留 WebRTC 直连和自身 WS/WSS relay 两条路径；公共与自建 STUN 辅助收集候选，Trickle ICE 及时交换候选，不启用 TURN。

请求 ID 由客户端生成，响应按 ID 匹配。后台有并发限制、请求超时和短期去重。没有透明重试写操作；用户取消或网络中断时，服务端可能已开始执行，客户端必须先刷新状态。新协议客户端应遵守相同规则。

## Codex backend

### 生命周期约束

当前桌面通过 C FFI 在 App 进程内运行 Go HostService / ClientService，不再启动 `agenthubd` 或通过本机 HTTP 调用自身。macOS 关闭窗口只隐藏，Flutter engine 与 Go goroutine 继续运行；菜单栏可重新打开窗口。明确退出时，原生入口调用 `AgentHubShutdown`，取消未完成请求、关闭连接并保存 SQLite，再退出进程。`internal/app/embedded_test.go` 和 `scripts/test-embedded-ffi.py` 验证直接调用、并发初始化、请求取消及重开后的设备身份持久化。

Codex App 与 Hub 直连同一个官方 app-server，不提供 Hub WS 网关。官方 server 以独立进程会话、文件日志运行，不继承 Hub 的 context；关闭 Hub 只断开 Hub 的 RPC 客户端，不停止官方 server。`codex-app-server.json` 私有记录保存 PID、进程启动时间、可执行文件和端点，下次 Hub 验证并复用同一官方 server；仍存活但无法连接时明确报错，不透明另起后端。

只有通过 Hub 启动的 App 才可接入：记录启动实例的 PID / 启动时间 / 路径，并核对 App 到这个 server 的实际 TCP 连接。没有启动记录、实例变化或连接断开时均不可控。macOS 当前通过只读 `ps` / `lsof` 检测，不读取进程环境或注入代码。单独手动启动的普通 App 仍使用其私有后端，需要在 Hub 中正常退出、重新接入。

初始化必须先确认后端可用，再启动带 `CODEX_APP_SERVER_WS_URL` 的 App；不得为了初始化远控失败而强制结束官方 App。若选择按默认方式启动作为降级，应移除该变量并明确标为未接入远控。后端崩溃后的重新启动不能保证原执行中任务无损恢复。

Provider 优先使用官方 App 的 bundled CLI，启动仅监听随机 loopback 端口的 `codex app-server --listen ws://...` 并完成 initialize/initialized。给官方 App 的 `CODEX_APP_SERVER_WS_URL` 直接使用同一端点，没有 `/desktop/<key>` 转发路由，旧 `desktopListen` 配置不再使用。

审批仅处理官方协议发到 Hub 自身连接上的 server request。移除网关后，不再拦截或镜像只发往 App 连接的审批；这些仍在官方 App 中处理。不能保证第三方能答复其它客户端专属请求。复杂请求仍提示在官方 App 处理。

直接读取历史不加载会话；发送指令时 resume 原 ID，再 start turn。独立 App 的私有 stdio 后端属于另一个进程，因此共享历史不代表能控制那个正在运行的 turn；必须先显式接入，writer conflict 不自动规避。

App 通过环境变量选择 WebSocket 的入口来自当前安装版本的实现，不是一个已确认长期稳定的公开配置文件契约。App 端握手和双端 UI 行为需要随版本回归。


当前打开的 Codex 会话使用通用 Provider 实时订阅接口：将原生消息文本增量、公开摘要和工具状态规范化后，通过已有加密连接长轮询读取有界事件日志。文本使用 UTF-16 偏移去重；初次接入、游标过期或重连时读取快照。原始推理、工具输出和原始协议通知不向此接口透传。
