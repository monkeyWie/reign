**Agents Anywhere 与 ReignHub：产品、架构与冷启动调查**

调查日期：2026-11-17。结论：ReignHub 有明确的轻量化、P2P、端到端加密和 Cloudflare 自部署差异，但还不能宣称全面领先。Agents Anywhere 已经有 Codex App / IDE IPC 接管、较完整的云端会话管理和生态分发渠道。应优先赢得“真实会话连续性、弱网首屏、重连和部署”这些核心体验，而不是同时追逐所有功能。

比较范围：Agents Anywhere 发布版 v2.2.0、源码 `98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008`；ReignHub 发布包 v0.1.0-rc.42、当前源码 `3481d72`、线上 `/chat/`。当前源码能力不等同于全部已发布能力。这里的远控是 Agent 的消息、审批、工具、文件和终端控制，不是完整屏幕桌面串流。

方法：读取源码、部署配置、公开发布资产、文档、README 提交历史、公开宣传文章及线上静态入口。没有安装运行竞品，没有登录其账号，没有操作真实 Agent 会话，也没有进行同设备的内存或端到端性能对跑。下文明确区分已验证事实、架构推断和建议目标。

**1. 核心比较**

| 维度 | Agents Anywhere | ReignHub 当前情况 | 判断 |
| --- | --- | --- | --- |
| 远控链路 | Client → Python Server → Connector → Agent；会话和附件经过或存储在 Server | 信令协调，原生端并行尝试 WebRTC/UDP、TCP；浏览器 WebRTC；失败走加密 WSS 中继 | 我们有绕过中继的机会，也承担更多网络状态和恢复复杂度；没有实测不能宣称更稳 |
| 服务端信任 | PostgreSQL 保存会话、Timeline 等，文件后端保存附件；应信任服务端运营者 | 配对与业务内容端到端加密，Worker 转发密文，服务器不持有聊天配对密钥 | 隐私和自托管是我们的明确差异；不等于整个产品已经通过独立安全审计 |
| 自部署 | Docker Compose：Server、PostgreSQL、Redis、迁移任务和文件存储 | 根仓库已有 Deploy to Cloudflare，Worker + D1 + 可休眠 Durable Objects；可选独立 Go 探测服务 | 我们的基础服务部署和运维组件更少；CF 不提供任意 UDP/STUN 服务，不能宣传全部 P2P 探测也在一个 Worker 内完成 |
| Codex App | 现有 App / IDE owner-follower IPC，附加自己的 app-server 路线 | Mac App 接入共享 app-server；Windows 当前仅检测 App，暂不支持 App 接入远控 | “能控制 Codex App”不是我们的独有卖点；无感接入现有 App 和 Windows 是差距 |
| Agent 接入 | Codex、Claude、DSH，v2.2 新增 Pi、Hermes；有 ACP | Codex App/CLI、Pi 原生 RPC、多种 ACP 适配及自定义 ACP 命令 | 不能用支持名单长度衡量；要逐个验证历史、审批、附件、恢复及原客户端状态一致性 |
| 会话历史 | 服务端持久 Timeline，多端可读取已上传历史 | 原生 SQLite、浏览器 IndexedDB；本地缓存可离线读，未缓存内容仍需工作设备 | 它在换新控制设备、电脑离线后的历史可达性上更有优势；我们减少服务端内容存储 |
| 项目聚合 | 服务端按用户、Connector、规范化 workspace 路径创建项目，UI 按 projectId 分组 | 被控端提供统一 projectDir / cwd 信息，控制端按 projectDir 优先聚合；Git common directory 支持 worktree 归并 | 我们刚完成的跨 Agent、跨 worktree 聚合有价值；双方都能让同目录的不同 Agent 聚到项目，不能说只有我们支持 |
| Web | Next.js / React Web，另有 Electron 桌面 renderer | NSGo / Go UI 编译为 WASM，原生和 Web 共享 | 我们复用高；竞品的 JS 路线有按需加载空间，我们的 WASM 首次准备是实际成本 |
| 平台与分发 | Mac/Windows 桌面，原生 Kotlin Android、Swift iOS；App Store/TestFlight；Linux Connector CLI | Go/NSGo 统一 UI，桌面、移动、浏览器及 Linux GUI 发布资产 | 我们覆盖和复用有优势；它有原生端适配和现成商店分发优势 |
| 周边能力 | 远程终端、文件预览/编辑、计划/目标、模型网关、账号与管理等较完整 | 主轴是轻量、安全的多 Agent 会话远控 | 功能完整度需要补齐核心闭环，不宜为了数量复制全部产品边界 |

依据：[项目 README][aa-readme]、[Server 架构][aa-server]、[部署配置][aa-compose]、[2.2.0 发布说明][aa-release]、[我们的当前 README][reign-readme]、[当前协议][reign-protocol]。

**2. 安装包、Web 与内存：哪些数字能比较**

公开资产的压缩安装包大小，单位为十进制 MB（1 MB = 1,000,000 bytes）：

| 平台 | Agents Anywhere v2.2.0 | ReignHub rc.42 | 注意事项 |
| --- | ---: | ---: | --- |
| Android APK | 43.55 MB | 9.67 MB，arm64 | 约 4.5 倍差距；ABI、内置依赖与功能范围不完全相同 |
| Windows x64 安装程序 | 328.66 MB | 8.26 MB | 同为 x64 的发布安装包，约 39.8 倍差距；不是安装后体积或内存比 |
| macOS DMG | 570.39 MB，universal | 7.41 MB，arm64 | 竞品含双架构，不能将比值当作同架构性能比较 |

精确字节：AA Android `43,548,660`，Windows `328,656,849`，Mac `570,386,582`；ReignHub Android `9,667,113`，Windows `8,257,667`，Mac `7,414,272`。来自双方 [Releases][aa-release] / [ReignHub rc.42][reign-release]，不是 README 中可能滞后的版本徽标。

差异有明确来源：竞品 Electron 桌面发布脚本会准备 Python、uv 和 Pi runtime，再打包 Electron；我们的 Go/NSGo 没有随包携带一套 Chromium。轻量安装包是可以用公开数据证明的优势。相应地，“是否已经自带运行某 Agent 所需的依赖”也不同，宣传时应说明 Agent 依赖。

Web 的情况不能照搬桌面结论：

- ReignHub 线上 HTML 清单：WASM gzip 合计 **6,957,792 bytes（6.96 MB）**，**14 个分片**，每个大分片 512 KiB，最后一个 142,048 bytes；下载器默认 **4 并发**，允许范围 1–6。当前先把分片下载成 Blob，再解压、编译和挂载；不是一边下载整个 WASM 一边让主界面完整可交互。
- AA 公开 Web 入口列出的 **20 个 JS 资源**，本次用 `curl --compressed` 实际接收的响应体合计 **993,997 bytes（约 0.99 MB）**。这个数字不含 CSS、字体、后续动态资源、登录后的 API 和完整工作台，不能叫“整个 AA Web 只有 1 MB”，也不是首次可用时间。
- 现有证据足以说明：**我们不能把“小桌面安装包”扩展为“Web 也更快”**。WASM 分片并发改善下载过程，但没有解决整个核心模块必须先就绪的门槛。优先检查前置 UI、Go 链接依赖、核心与非核心功能拆分、长缓存命中率和编译耗时；提高并发数不是唯一解。
- ReignHub 的 `web-worker.tar.gz` 是部署归档，不能拿它的大小当浏览器下载量。

内存目前没有可发布的横向结论。本机 ReignHub dev.134 的两次桌面进程 RSS 快照约 **123 MiB → 78 MiB**，页面是控制台，未计入独立 Codex/其他 Agent 进程，足见单点数据会变。竞品没有在本机同场景实测。它的 `worker-memory-466.md` 中 224 MiB → 94.55 MiB 是服务端队列合成实验，不能用来与我们的桌面 RSS 对比。Electron 有更大的运行时基础成本是合理推断，具体节省比例仍需测量。

正式对跑需固定同一机器、同一 Agent、同一历史量，分别记录：冷启动至可用、空闲/流式/大历史 RAM、所有所属子进程、Web 冷/热缓存、有限带宽和丢包下首条正文出现时间。原始值、场景、版本要一起公布。

**3. Codex App：值得优先借鉴的实现**

已核对当前活动源码，而不只读方案文档：`runtime.py` 启动 IPC；`provider.py` 的 `ipcEnabled` 默认 true；`ipc/control.py` 处理跟随线程的 start/steer/interrupt/approval；`ipc/owner.py`、`follower.py` 和 `versions.py` 处理双向 owner/follower 及版本。Unix 使用 `$CODEX_HOME/ipc/ipc.sock`，Windows 有 `\\.\pipe\codex-ipc` 路径。

它区分了两个东西：CLI 的共享 app-server daemon，与 App/IDE 的本地 IPC router。App/IDE 已经持有会话时，让原 owner 代为执行；没有 writer 时才恢复到自己的 app-server；writer 存在但 owner 不可达时，有受控的 deeplink handoff，并避免两个写入方同时接管。只读历史不应顺手 resume 抢占会话。

这比“把历史读出来，然后另起一个后端继续聊”更贴近真正接管，也比我们需要 App 接入共享后端的初始化体验更自然。我们应优先研究：**不重启用户正在使用的 App、保持原窗口持续更新、同一条任务审批只执行一次**。

但有三个边界：这是对版本敏感的私有 IPC，需要维护方法版本和 owner/router 生命周期；有 Windows 命名管道实现不等于所有 Windows 版本都已端到端验证；仓库公开验收记录明确有双端和故障场景未完成，不能替它背书为“全部稳定通过”。部分旧迁移文档写 IPC 已移除，与当前重新引入的活动源码不同，应按当前源码判断。

我们当前 Mac 共享后端本身也有环境入口的版本依赖，不能简单宣传成“我们全部官方稳定、对方全部逆向脆弱”。应保留共享后端作为可控路径，评估 IPC 附加路径，并以真实 App / IDE / Windows 的能力矩阵和回归测试约束承诺。

依据：[IPC 调研][aa-ipc]、[活动 runtime][aa-runtime]、[IPC 控制代码][aa-control]、[公开验收边界][aa-ipc-qa]、[我们的 runtime][reign-runtime]。

**4. 会话管理与弱网：借鉴机制，不照搬存储架构**

AA 的中心 Timeline 模型优点是多端一致、离线工作机的历史仍可达、列表能直接读服务端最新摘要。代价是会话先上传 Server 才能被其他端读取，数据存储、数据库、Redis 和备份升级都由服务运营者承担。我们的 host 直达模型少一道中心内容存储，LAN/P2P 可以更短，但手机初次打开未缓存历史时，工作机必须能提供数据。

它的项目关联在 Server 完成：`_ensure_project_for_workspace` 以用户、Connector 和规范化路径找/建项目，会话 upsert 传入 cwd。相同目录的多个 runtime 可归到同一项目；在审阅的归属代码中没有看到通用 Git common directory 的 worktree 主项目推导，因此不能假定它会自动把所有 worktree 归为主仓库。我们当前方案保留实际 cwd，把 projectDir 独立为通用元数据，再由控制端聚合，适合跨 Agent 的一致展示；也应保留以后显式指定项目边界的空间。

它的 [会话缓存约定][aa-cache] 有几个可直接借鉴的产品约束：

- 返回已有会话立即用缓存呈现，从已经应用的事件游标追增量；历史分页不能错误推进实时游标。
- 过期响应不能覆盖其他会话或同一会话的新一次访问；恢复失败保留已有可读内容。
- 向上阅读时不自动拉到底部；模型目录失败不阻止阅读历史；离开页面释放订阅。

但文档规定 Web/Desktop/Android 当前是最多 5 会话、约 16 MiB 字符串估算的**内存缓存**，不承诺刷新或进程死亡后保留；iOS 另有落盘机制。我们的 SQLite/IndexedDB 持久缓存有不同优势。AA 的服务端持久历史与客户端内存缓存也不能混为一谈。

它的 v2.2.0 发布说明还列出了：大历史分批上传、故障会话指数退避隔离、流式条目旧版本合并、列表最新摘要、附件流式传输、降低数据库锁和连接池压力。也明确修复过会话写入卡数小时、某些 Redis 场景约 5 分钟无消息等问题。这能证明它面对类似难题并投入了修复，不能证明当前弱网体验已优于我们。

我们的当前 RPC v3 已有 4 KiB 片、按帧调度、live/control 优先、每 RPC 接收隔离、压缩和单请求无进展超时；有序历史按正文片段逐步显示。需要验证的是这些机制在真实公网中继、前后台切换和大历史下的**最终表现**。WSS 仍依赖 TCP，应用层多路并发不能消除底层 TCP 丢包的队头等待；不能宣称“多路复用后任何网络都不卡”。

**5. 它是如何冷启动和宣发的**

截至调查，GitHub 约 **3,484 stars / 297 forks**。下面是可公开追溯的时间线，不是完整增长归因：

| 时间 | 可验证动作 | 能借鉴什么 |
| --- | --- | --- |
| 2026-05-22 | 仓库创建；初期 README 用手机接管 Agent 的具体场景、桌面/手机截图、beta waitlist 建立入口 | 先让用户理解价值、看见效果、找到下一步；当时“每周 200 名”是营销文案，不是已验证活跃数 |
| 06-27～07 月初 | 首批 Android 和桌面发布包，随后逐步更新 | 可下载成品比只有架构和命令更容易转化 |
| 07-04 | BensonWang 掘金首发介绍手机控制 Claude/Codex、终端、文件、自部署；文章自述当时 300+ stars | 一篇明确的场景演示 + 安装入口 + 安全边界说明；不是堆协议名词 |
| 07-09 | README 默认切中文 | 有意降低中文受众理解门槛 |
| 08 月以后 | 同组织 DSH Desktop 项目和后续 AA bridge 插件形成分发入口 | 与正在被使用的 Agent 产品结合，比单独做遥控器获客更自然 |
| 09-18 | 贡献者 xipian1216 发布 CSDN 上手文章，覆盖多端、Connector、项目、终端文件 | 开发成员持续写真实使用教程；不是一次性发完就等 stars |
| 09-24 | 掘金出现“四款 DSH Desktop 测评”，突出手机远控和故障恢复 | 功能随生态产品被讨论，带来额外曝光；没有数据证明具体转化量 |
| 09-27 | README 首次加入阿里云无影、88API 赞助卡片 | 从建仓到首个公开赞助位约四个月，不是刚发布就立刻拥有成熟商业收入 |
| 11-16 | PR #663 增加七牛云和 Fenno.ai 赞助位 | 持续运营 README 广告/合作入口 |

DSH Desktop 在调查时约 **36,186 stars**，与 AA 在同一组织，并有内置/插件连接入口。这是可见的生态分发渠道；不能倒推它在 5 月就有这批流量，也不能将 AA 所有 stars 归因于它。官网还提供微信/QQ 社群、国内下载镜像、App Store/TestFlight、企业私有部署和定制咨询入口，说明它在经营持续触达与商业线索。

已找到的文章：[7 月掘金介绍][launch-juejin]、[9 月 CSDN 教程][launch-csdn]、[DSH 对比文章][launch-dsh]。没有确认其 Product Hunt / Hacker News 首发，也没有可靠的逐日 stars 归因数据，不能编出一条“某篇帖子引爆增长”的故事。

**赞助究竟意味着什么：** 当前 README 有阿里云无影、88API、七牛云、Fenno.ai。88API、Fenno 链接有 `aff`，七牛有项目 UTM，阿里云落地页在 dshdesktop.cn；Fenno 文案明确含邀请返佣。这只能证明存在公开合作/推广位及可归因链接。看不到合同、赞助金额、现金或额度、固定广告费或联盟佣金的具体结构，不能把四个 logo 解读为四笔大额现金赞助，也不能认定都是 AA 单一产品独立谈来的。

依据：[最初 README][initial-readme]、[首次赞助提交][sponsor-first]、[新增赞助 PR][sponsor-pr]、[DSH Desktop][dsh]、当前 README。

**6. ReignHub 要怎么打出来**

不需要每一项都“全方位碾压”。Electron 换成 Go、不存明文、支持 P2P，都是选择；用户最终关心的是离开电脑后能不能继续同一任务、消息有没有及时出现、失败后是否自动恢复。此前用户实际遇到的同步无反馈、重连卡住和移动端切后台断连，比再加几个 Agent 名称更影响首批留存。

建议定位：**轻量、端到端加密、直连优先，接着控制电脑上原来那条 Agent 会话。** 只有对应平台和 Agent 真正做到原会话接管后，才在对外材料里承诺；现阶段不能笼统宣传 Windows Codex App 已支持。

建议优先顺序：

1. **稳定性和可感知进度。** 用同一份长历史、实时输出和附件并发验证中继；看首条 Agent 正文出现时间，而非只看连接图标变绿。断网、App 后台恢复、网络切换均验证不会卡在旧 attempt，不自动重放结果不明的写操作。
2. **Codex App / IDE 的真实接管。** 研究现有 IPC owner/follower 路线，补 Windows；测试原窗口和手机双端同步、审批唯一执行、owner 退出恢复。能列历史不算完成接管。
3. **Web 首次可用。** 分开记录 HTML 到达、资源下载、解压、WASM 编译、挂载、缓存首屏、网络历史首片；必要时先让非核心界面可用。不能只继续改进度条或调高并发。
4. **一键部署和新用户闭环。** 在全新 CF 账号实走部署、配对、接管、移动端继续任务；公布 CF 资源边界、可选探测服务和大陆网络限制。按钮存在不等于所有新账号已验证零阻碍。
5. **再做增长。** 录制 30–60 秒真实演示：桌面 Agent 正在工作 → 手机审批/追加指令 → 桌面实时变化；提供一页入门和一个下载入口。做中英 README、弱网/体积可复现对比、Agent 社区教程及可安装插件；每次发布围绕一个可见场景。赞助材料以真实下载、活跃部署和匿名聚合转化数据为基础，区分固定赞助、云额度和联盟推广。

建议的初始验收目标，**不是当前实测成绩或对所有公网环境的保证**：缓存首屏 p95 ≤ 300 ms；健康 LAN 下已配对设备恢复至可操作 p95 ≤ 2 s；10 秒内无建连进展则终止该次拨号并切换候选/重试；已建立的中继在 128 kbps、150 ms RTT、1% 丢包的固定测试模型下，轻量实时消息 UI 延迟 p95 ≤ 3 s，并在大历史同步时保持可交互、有序渐进显示。10 秒拨号预算不能误用于不断有进展的大历史传输，也不能通过无限快速重试加重拥塞。

要做公开性能承诺，先实现同机自动化基准、保存原始数据，再决定最终阈值。AA 现有更完整的功能和渠道值得认真对待，我们现有的体积、隐私和部署优势也足够形成清晰定位；核心体验达到可验证的优势后再放大宣传。

**证据入口**

以下竞品源码链接固定到本次审阅提交；发布资产、线上 Web 与 stars 会继续变化。

[aa-readme]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/README.md
[aa-server]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/docs/server-architecture.md
[aa-compose]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/docker/docker-compose.postgres.yml
[aa-release]: https://github.com/anywhere-labs/Agents-Anywhere/releases/tag/v2.2.0
[aa-cache]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/docs/session-cache.md
[aa-ipc]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/docs/codex-ipc-takeover.md
[aa-runtime]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/connector/connector/runtimes/codex/runtime.py
[aa-control]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/connector/connector/runtimes/codex/ipc/control.py
[aa-ipc-qa]: https://github.com/anywhere-labs/Agents-Anywhere/blob/98dd0e6e5ddc1c5d1b35e1aaea6b5d00bdfd7008/docs/codex-ipc-computer-use-checklist-2026-10-20.md
[reign-readme]: https://github.com/monkeyWie/reignhub/blob/3481d72/README.md
[reign-protocol]: https://github.com/monkeyWie/reignhub/blob/3481d72/docs/platform/protocol.md
[reign-runtime]: https://github.com/monkeyWie/reignhub/blob/3481d72/internal/codex/runtime.go
[reign-release]: https://github.com/monkeyWie/reignhub/releases/tag/v0.1.0-rc.42
[initial-readme]: https://github.com/anywhere-labs/Agents-Anywhere/blob/1b5d8918c78dd9ebb663a81f74bc30ae91c73084/README.md
[sponsor-first]: https://github.com/anywhere-labs/Agents-Anywhere/commit/b6fe36366e2a4d5695795474e5b86e441b675218
[sponsor-pr]: https://github.com/anywhere-labs/Agents-Anywhere/pull/663
[dsh]: https://github.com/anywhere-labs/dsh-desktop
[launch-juejin]: https://juejin.cn/post/7658185170865979426
[launch-csdn]: https://blog.csdn.net/xipian1216/article/details/165885814
[launch-dsh]: https://juejin.cn/post/7688669403517566986
