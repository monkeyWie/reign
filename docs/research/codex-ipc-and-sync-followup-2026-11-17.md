**Codex IPC 与同步机制：可借鉴部分及落地边界**

2026-11-17，对上一份竞品报告的源码复核。本文是实施建议，尚未替换 ReignHub 的运行时或操作真实 Codex 会话。

**结论**

应跟进 Codex App / IDE 的 owner-follower IPC，作为现有会话的首选接入路径。它的用户体验优势是无需先重启原 App 改接共享后端。需要同时完成写入归属判断、版本适配、双向 owner/follower 和恢复机制，单独实现一个 socket 客户端并不足以交付。

事件游标、大历史分批和故障会话隔离均有参考价值。我们已有前两者的基础，不宜重新造一套；优先补按会话恢复、持久化一致性、错误分类和退避隔离。

**1. 对方的 IPC 怎样工作**

本地链路：ReignHub 可采用与 AA 类似的 `Provider → IPC follower → Codex App/IDE owner → 原 app-server`。远端手机/浏览器仍通过原有加密传输访问 ReignHub；IPC socket 不向公网暴露。

- Unix：`$CODEX_HOME/ipc/ipc.sock`；Windows：`\\.\pipe\codex-ipc`。
- 帧是 4 字节小端长度加 JSON。请求、响应、广播带方法版本；IPC router 负责发现和路由。
- `thread-owner-discovery` 寻找持有线程的窗口；`thread-stream-following-changed` 宣布跟随。
- `thread-stream-state-changed` 下发初始 snapshot 和后续 JSON Patch；patch 的 `baseRevision` 必须与已应用 revision 相同，不同则重新请求 snapshot。
- 发消息、追加、停止、修改设置、审批经 `thread-follower-*` 发给原 owner，由它操作原 app-server。
- IPC 断连后重连并重新跟随。owner 切换后不能沿用旧 owner 的 revision。
- 自己创建/持有的线程还需实现 owner 一侧，让 App/IDE 也能跟随；否则“手机先建会话，再在 App 打开”仍可能形成两个 writer。

AA 活动代码：[`runtime.py`](https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/connector/connector/runtimes/codex/runtime.py)、[`follower.py`](https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/connector/connector/runtimes/codex/ipc/follower.py)、[`control.py`](https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/connector/connector/runtimes/codex/ipc/control.py)。

**2. 会不会出现“会话被占用”**

正常跟随已打开 App 会话时，不需要第二个 app-server `thread/resume`，所以能避免这条路径产生的双 writer 争抢。不能保证占用错误彻底消失。

AA 的生产 Provider 明确启用 `safe_ipc_write_routing=True`，路由遵循：

| 状态 | 行为 |
| --- | --- |
| 现有 owner 可达 | 通过 IPC 请求 owner 执行 |
| Connector 自己持有 | 使用自己的 app-server |
| 无外部 writer | 可以在自己的后端恢复，仍以 Codex 最终结果为准 |
| 外部 writer 存在，owner 暂不可达 | 有界发现/等待；可尝试把会话在正在运行的 App 中打开，再跟随 |
| 仍有外部 writer，恢复 owner 失败 | 返回明确的暂不可控错误，不再降级到自己的后端硬恢复 |

它检查的是真实文件锁，而非 `.lock` 文件是否存在。Unix 非阻塞 `flock` 探测后立即释放；Windows 有文件字节锁探测。探测与实际 resume 之间存在竞争窗口，Codex 的最终写锁仍是权威。它自己的 SDK 路线也明确处理 `already has an active writer`，因此这不是一个“绝不会冲突”的方案。

还应区分两件事：会话有人持有，与会话正在生成。前者决定找谁执行，后者决定允许追加、排队还是停止。不能因为 owner 正在生成就去启动第二个 writer。

特别注意：AA 的 deeplink handoff 使用 `codex://threads/<id>`，会让原 App 导航/打开这个会话。它不是完全无副作用的被动观察；不应在每次只读浏览失败时自动触发，更不能默认杀掉 App 释放锁。

我们当前 `Transcript` 读取不主动 resume，但 `internal/codex/stream.go` 的 watch reset 会调用 `threadClient`，后者可能 `thread/resume`。共享后端正常接入时不一定冲突；加入外部 App/IDE owner 后，必须把“只读历史/订阅”和“获得执行路径”分开，避免打开聊天就尝试占用线程。

写入超时还可能是结果不明：原 owner 可能已执行，只是响应丢失。不能超时后换一个后端自动重发；保留请求身份、核对线程事件，并向用户区分“未发送”和“结果待确认”。

**3. 本机已验证的协议版本差异**

只读取本机安装包/扩展静态资源，没有连接其 IPC，也没有发送消息或恢复真实会话。

| 实现 | 版本 | `thread-stream-state-changed` 方法版本 |
| --- | --- | ---: |
| 本机 Codex App（ChatGPT.app） | 26.1106.51428 | 11 |
| 本机 VS Code Codex 扩展 | 26.1111.21308 | 13 |
| AA 本次审阅的 `ipc/versions.py` | 源码 98dd0e6 | 11 |

本机 App 和扩展的 queued-followups 两个方法版本均已变为 3，AA 表中为 1/2。AA `client.py` 的广播接收会丢弃不匹配的方法版本，因此照搬这份静态表存在漏掉新扩展状态广播的风险。这是静态代码发现，不是对竞品在本机的实跑故障结论。

落地时按安装版本选择经过验证的协议适配，校验每个 owner 的消息版本和结构；同时存在不同版本 App/IDE 时不能只设一个全局版本。未知结构必须显示明确的能力限制，不能静默丢弃后一直显示同步中，也不能仅把整数 11 改成 13 就假定 JSON Patch 结构兼容。

**4. 三项同步机制如何借鉴**

| 机制 | 我们已有 | 值得补的部分 |
| --- | --- | --- |
| 事件游标恢复 | `epoch/cursor/reset`、事件重放、初始历史与实时事件合并 | 每会话独立恢复点；消息内容与已持久应用游标一起提交；短暂控制端断线只补缺口 |
| 大历史分批 | 最新 30 turns、正文 2 KiB 渐进片、传输 4 KiB 帧、压缩与优先级调度 | 按字节预算控制上游读取和应用页；页完成后保存进度；失败从页边界恢复，保持原顺序 |
| 故障会话隔离 | 请求取消、每 RPC 收件隔离、UI 生命周期隔离 | 会话/操作级错误分类、退避和有界探测；单会话历史失败不影响目录、其他会话和实时流 |

**游标的具体改进。** 当前 `sessionStream` 的恢复位置主要在 State 的单个 `streamKey/epoch/cursor`；换会话会重新初始化。Provider 的重放日志按 provider 共用，最多 1024 条/2 MiB，忙碌的其他会话也会消耗窗口；后端连接替换会 reset。可以增加按会话的有界日志和持久检查点，并保留总容量限制。持久化的内容、游标、source generation 必须一致：收到游标不等于已应用，更不等于已落盘。旧记录不能直接复用到新 owner。真正丢失重放窗口时才重建受影响的会话。

**历史分批的具体改进。** AA 的 `snapshot_body.py` 把历史编码到临时文件，按条目边界分页面上传，过大响应时缩小后续页，成功应用后才提交源检查点；页面有顺序范围，不把局部页当整会话删除。值得借鉴这些语义，但它默认 45 MiB 的页是 Connector→Server 上传参数，不适合直接搬到手机弱网下行。

我们当前 `thread/turns/list` 虽限制 30 turns，但它仍先获得上游整页，再把页面内消息切成正文片发给控制端。30 turns 并不保证字节小；超长工具输出仍可能拖慢上游读取/解析。应利用上游实际支持的分页和精简视图，限制初始数据量；支持时逐步读取 item。已有的正文片和帧调度继续复用。IPC 的初始状态也可能很大，接上 IPC 不会自动解决大历史延迟。

**隔离的具体改进。** 我们的历史和 watch 失败目前有固定每秒重试；持续解析失败、源数据异常等错误可能反复消耗资源。AA 的后台永久拒收退避约从 10 分钟开始，逐步到小时级；它仍更新目录与状态。这适合后台归档补传，不能照搬给用户当前正在看的会话。

建议分层：网络故障触发连接级快速候选切换；暂时忙碌按提示或短退避；正文解析/超限在该会话的历史任务内隔离并尝试小页；写锁问题等待 owner，不重试整个设备连接；写入结果不明禁止自动重放。当前会话保留缓存、已有正文和重试入口，网络恢复/用户重试/源版本变化时允许提前探测。

还应测一类具体风险：初始历史很慢，同时实时输出不断增长。当前 UI 的初始事件 journal 超过 1024 条/2 MiB 会使本次历史失效并重新获取；若持续追不上，可能反复重建。可通过有序落盘、合并同 item 的重复状态、缩小基线及明确水位来控制，不应无限扩大内存队列。

**5. 建议实施顺序与验收**

先把外部 owner 路由从读历史、watch 和写操作中独立出来，加入 IPC App 跟随；补 owner 侧双向接入、IDE 版本适配、Windows 命名管道及 router 生命周期。现有共享后端保留给真正由我们管理的运行时，不让两条路径同时操作同一会话。

并行设计通用的会话恢复状态，供 Codex、ACP、Pi 复用；在现有协议上补按会话持久检查点、历史页恢复和故障隔离，不改成中心服务器保存明文聊天的架构。

关键验收：

- 原 App 正在生成时，手机可观察、追加/停止，原窗口继续更新，同一命令只执行一次。
- 浏览会话不创建第二个 writer；owner 不可达且锁仍持有时及时返回，不硬 resume。
- 手机创建后 App 打开、App/IDE 多窗口、owner 退出、router 重启、不同协议版本均验证归属。
- 网络丢失 ACK 时不重复发送；审批在原 App 已处理后另一端及时清除。
- 大历史同步期间消息顺序正确、正文渐进显示，停止/返回仍可操作；一条坏会话不影响另一条。
- 断网、切后台、杀控制端进程后恢复，游标与已保存正文一致；超出重放窗口只重建对应会话。

这些是实施与测试范围，尚不能据此承诺已完成真实 App/IDE/Windows 端到端验证。
