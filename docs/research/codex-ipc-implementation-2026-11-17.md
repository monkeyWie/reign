# Codex App IPC 实现与同步恢复验收

日期：2026-11-17。采用新架构，不保留共享 WS 后端的运行时兼容或迁移。

## 已实现

- App 已有会话：通过现有本地 IPC router 发现 owner，跟随 snapshot / JSON Patch，发送、追加、停止及审批交给原 owner。仅只读访问不获取会话写锁。
- ReignHub 新建会话：自己的私有 stdio app-server 持有写锁，向 App follower 发布状态，接收 App 输入、排队消息、停止、设置与审批。新 follower 先拿快照，之后发布细粒度 patch。
- 归属保护：owner 不可达时验证真实 OS 写锁；锁被持有或发现失败时不强行 resume。关闭 ReignHub 只关闭自己的执行器和 IPC，不关闭 Codex App。
- 写入语义：稳定消息 ID 的有界结果账本、重复消息去重、ID 与正文一致性检查、审批一次性认领。超时、ACK 丢失、断线均不触发自动重发或换后端执行。无法确认的排队消息需要显式移除；同一个未确认 ID 不重新执行。
- 状态恢复：owner 来源/host/版本/会话 ID 校验，旧 revision 丢弃，patch 缺口重新请求基线；router 复位后重连、重新跟随；watch 在 owner 丢失后重新发现。
- 进程生命周期：父进程退出关闭 stdin，私有执行器退出并释放锁；优先给执行器 EOF 退出机会，超时才结束自己拥有的子进程。Agent 初始化移到后台，不阻塞本地设备服务启动。

## 同步与失败策略

- 事件重放按会话隔离，每会话最多 1024 条/2 MiB，所有会话共最多 8 MiB/128 个。大输出优先挤掉自己的记录；只有丢失该会话事件或 epoch 变化才重建基线。
- 已应用的 cursor 与消息正文放进同一个本地快照。切换会话、短暂重连及读取本地缓存时可从各自检查点补增量。未完成历史不提交恢复 cursor。
- 默认最新 30 turns，正文保持 2 KiB 渐进片。私有 owner 完整历史采用分页，每一成功页将内容与 nextCursor 一起提交；后续页失败从成功页边界继续。总历史 24 MiB 预算、重复游标检测、明确超限错误。
- 读取历史期间不覆盖更新的实时 token 或完成状态。私有 owner 发布 4 路并发，失败只退避当前会话。
- 控制端历史读取预算 30 秒。网络错误约 0.8–10 秒指数退避；忙碌/写锁约 1.6–30 秒；数据结构/超限错误约 12–120 秒；其他读取错误约 4–60 秒。附有抖动，成功清零，重新进入或连接恢复创建新探测。
- 这套退避只用于幂等读取。连接建立仍使用原有单次 socket 10 秒、setup 30 秒及有界快速重试；发送和审批不会套用读取重试策略。
- 本地 IPC 和 stdio 的发送锁等待可取消，避免请求在另一个阻塞写入后继续排队超过自身期限。

## 验证

通过隔离的 net.Pipe、模拟 router、JSON-RPC backend 和子进程执行，不向真实会话发送模型请求：

- App 会话发消息/只读订阅不会访问第二个 writer；发现失败且真实 flock 被持有时禁止 resume。
- 发送结果不明不重放；重复消息 ID 只执行一次，换正文被拒绝；权限审批只答复一次。
- 错误 owner、未知 schema、旧快照拒绝；revision 缺口触发重取；Unicode 文本偏移使用 UTF-16。
- 新 follower 首先收到 snapshot，再收到 patch；JSON Pointer 转义、数组增删与细粒度文本 patch 往返一致。
- 慢历史读取不能回滚实时文本/标题；历史分页失败后不重复下载成功页。
- 大输出会话不挤掉安静会话的恢复窗口，日志总容量和会话数受限。
- 本地保存的消息内容与 cursor 匹配，能补实时增量；未完成基线不提交恢复点。
- 执行器关闭后不可复用；杀父进程后子进程退出并释放真实写锁。
- 发送锁排队时取消立即返回，不等待前一写入完成。

检查命令：

```sh
go test ./...
go test -race ./internal/codex ./internal/codexipc ./internal/rpc ./internal/provider ./internal/readretry
(cd clients/nsgo && GOWORK=off go test ./...)
(cd clients/nsgo && GOWORK=off go test -race ./hubui -run 'TestSavedSessionCursor|TestLiveText|TestSessionWriterConflict|Test.*Snapshot|Test.*History')
GOOS=windows GOARCH=amd64 go test -c ./internal/codex -o /tmp/reign-codex-ipc-windows.test.exe
(cd clients/nsgo && GOWORK=off GOOS=js GOARCH=wasm go build -ldflags=-checklinkname=0 -o /tmp/reignhub-ipc-web.wasm .)
(cd apps/site && npm run check)
```

## 尚未验证与能力边界

本轮没有操作真实 Codex App 会话，也没有发送实际模型消息。此前计算机工具拒绝访问 Codex App，本轮未用本地 IPC 绕过该限制进行实机操作；安装/重启仅针对 ReignHub 自身。真实 App 的多窗口、原 App 正在生成时双端操作、退出后恢复及 Windows 命名管道运行仍需要实机验收，不能宣称“完美闭环”。

当前实现 App state schema v11，不接受 IDE state schema v13。此内部协议会随官方版本变化，未知版本明确失败。App follower 的编辑历史和 daybreak 等扩展操作未实现，会明确报不支持。复杂排队附件/富上下文只有已识别的结构能发送，其他形状明确拒绝，不静默丢内容。通用控制端尚未提供所有复杂审批的专用 UI，原 App 可通过 IPC 回答已实现的权限和 MCP elicitation 请求。

单个上游 turn 仍可能很大：30 turns 是条数限制而非网络字节保证；上游整页返回前不能凭空产生其内容。正文渐进协议、帧调度与读取预算改善反馈，不代表彻底消除上游大页等待。初始 IPC 快照也受 32 MiB 帧限制。

ReignHub 新建会话由其私有后端执行，明确退出可能中止任务。写锁释放只是允许 App 后续尝试恢复，无法保证原任务无损延续。

## 本机开发版

已构建、安装并重新启动 `0.1.0-dev.20261117.8`（build 135），运行路径 `/Users/levi/Applications/ReignHub.app/Contents/MacOS/ReignHub`。安装后的执行文件 SHA-256 与构建产物一致，签名校验通过。旧版程序保留在 `/Users/levi/Applications/ReignHub.backup-ipc-20261117-131857.app`。没有发布 RC，也未提交或推送代码。
