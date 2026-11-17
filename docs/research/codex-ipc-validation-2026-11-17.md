**Codex App 纯 IPC 接入验证**

2026-11-17；对象为本机 Codex App（`/Applications/ChatGPT.app`）26.1106.51428。范围是 App 已运行时的远控，不包含退出 App 后继续执行。

**结论**

当前安装版本的 owner/follower IPC 有现有会话的发送、停止、审批、状态跟随和历史加载接口；这些操作由原 App 转给它自己的后端执行，ReignHub 不必为了控制已有会话再启动一个 app-server。

但不能据此宣布完整远控可以仅靠 IPC：本次检查的 owner 请求注册表没有会话目录、搜索、新建会话等通用接口。也没有发现把任意 `thread/*` 请求透传给 App 内部 app-server 的入口。已有会话控制的接口存在与完整工作流实测成功，是两个不同结论。

**证据与能力边界**

| 能力 | 当前安装包中的证据 | 本轮验证程度 |
| --- | --- | --- |
| 发现已持有会话的 App 窗口 | `thread-owner-discovery` | 原始注册函数的接收/拒绝分支已隔离测试 |
| 发送消息 | `thread-follower-start-turn` | 原始转发函数已隔离测试；未执行真实模型 |
| 追加、停止 | `thread-follower-steer-turn` / `interrupt-turn` | 注册存在；停止转发已隔离测试 |
| 工具/文件审批、补充输入 | 多个 `*-approval-*` / `submit-user-input` 方法 | 注册存在；命令审批转发已隔离测试 |
| 跟随实时回复 | `thread-stream-following-changed` / `thread-stream-state-changed` | 静态确认 snapshot/patch 路径，未验证真实事件 |
| 已知会话的完整历史 | `thread-follower-load-complete-history` | 原始转发函数已隔离测试；未验证真实历史内容 |
| 排队消息 | `set-queued-follow-ups-state`、remove、clear | 注册存在；设置队列转发已隔离测试 |
| 会话列表、搜索 | 未找到对应 owner IPC handler | 当前注册表不覆盖 |
| 新建会话并返回 ID | 未找到对应 owner IPC handler | 当前注册表不覆盖 |
| 对任意历史会话直接分页读取 | 未导出 `thread/read` / `thread/turns/list` | 不能把已有会话的完整历史接口视为通用分页接口 |

发现需要已知 conversationId；App 正在运行不意味着每条历史会话都有可达 owner。发送必须经过实际持有该会话的 App/IDE；不可达时不能为了重试就启动第二个 writer 强行 resume。

进一步静态核对了 App 自带的发送端代码：`739301.37a39f26d6.js` 的发送路径读取 `getStreamRole`，并可调用 `sendThreadFollowerRequest(..., "thread-follower-start-turn", ...)`；队列路径使用 `request("thread-follower-set-queued-follow-ups-state", ..., {targetClientId})`。因此 Connector 接收 App 输入的机制有 App 端原生代码支持：注册自己为某会话的 owner，App 成为 follower 后主动通过 IPC 请求该 owner。无需截获键盘或修改 App 输入框。这里确认的是安装包里的发送分支，真实会话是否成功进入该分支及版本是否匹配仍需端到端验收。

完整历史接口在 App 端会请求 `getCompleteConversationTurns` 后发送状态，并设置长等待；它不自动解决大历史、弱网下的分页和首屏延迟。

**隔离测试如何进行**

从本机安装包提取原始 owner 注册函数，在 Node VM 中执行；只注入模拟的会话持有判定和模拟执行器。通过临时 Unix socket 发送长度前缀 JSON 帧，覆盖拆开的帧头/正文、初始化、owner 发现、五类请求转发，以及五种未注册请求不被转发。

结果：14 项通过、0 项失败。执行器只返回测试标记，没有连接真实 App socket，没有调用模型，也没有修改真实会话。未注册方法返回 `no-client-found` 是测试 router 的行为；它证明本次注册表没有该 handler，不能当成真实 App 返回值。

可复跑：

```sh
node docs/research/verification/codex-ipc-2026-11-17/validate.cjs
```

脚本仅从本地 ASAR 读取指定代码，并核对 SHA-256；App 升级导致代码变化时会明确失败，不自动猜测新协议。仓库没有复制安装包源码。

- [验证脚本](verification/codex-ipc-2026-11-17/validate.cjs)
- [完整测试结果与 18 个方法清单](verification/codex-ipc-2026-11-17/results.json)
- [版本与源码摘要](verification/codex-ipc-2026-11-17/manifest.json)

**是否能做到不额外启动 app-server**

这是比“纯 IPC”更宽的目标，仍值得继续验证。目录可研究读取本地索引，已有会话执行走 IPC；但这会依赖本地存储格式，也仍要解决新建和历史会话激活。

安装包确实解析 `codex://threads/new`，接受 prompt、path 等参数，转换成 `newThread` 路由。这只能证明深链入口存在，未证明可以无人操作地创建、提交消息并可靠返回新 ID。它属于 IPC 之外的入口，还可能改变 App 的当前窗口，不能作为已通过的新建方案。

因此本轮既不能承诺纯 IPC 足够，也不能断言必须再启动 app-server。明确可优先采用的是“原 App 持有的会话通过 IPC 控制”；全功能替换需补齐目录、新建和历史会话激活的证据。

**落地时的两个重点**

1. 协议版本不能照搬竞品。本机 App 的状态流版本为 11，IDE 为 13；本机 App/IDE 的队列设置、队列变化方法均为 3，竞品审阅版本仍为 1/2。需要按 owner 能力和结构适配，不能仅修改版本数字。
2. 观察不获取写入归属。ReignHub 当前 watch reset 可能通过 `threadClient` 触发 resume；接外部 App 时需把只读历史、订阅和执行路由拆开。写入结果不明时不自动换后端重放。

**未完成的真实验收及原因**

真实 App 发现、实际发送/停止/审批、真实 snapshot/patch、手机创建后 App 继续输入，以及多窗口是否出现占用问题，均未通过端到端验收。

电脑控制工具拒绝访问 `com.openai.codex`，原文为 `Computer Use is not allowed to use the app 'com.openai.codex' for safety reasons.` 本轮因此只做离线安装包分析与独立模拟服务测试，没有改用真实 IPC 去绕过这项限制。

本轮未修改产品运行时，未重启本地应用，也未发布 RC。
