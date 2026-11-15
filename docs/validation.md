# 本地验收记录

## 2026-11-15 Windows Codex App 检测

- Windows amd64，Go 1.27.3：`go test ./internal/system ./internal/codex ./internal/provider` 与 `go vet ./internal/system ./internal/codex` 通过。
- 显式设置 `REIGNHUB_TEST_INSTALLED_CODEX=1` 后运行 `TestInstalledCodexReadOnly`，识别 `OpenAI.Codex` 26.1027.3384.0、清单入口 `app/ChatGPT.exe`、包内 `app/resources/codex.exe`，仅计 1 个桌面主实例。没有启动或重启真实 App / Agent，也没有发送模型请求。
- NSGo 的 `TestInstalledUnsupportedAppDoesNotOfferEnable`、窄屏变体与 `TestDesktopNavigationAndDrafts` 通过：已安装但不支持接入的 App 显示原因并隐藏启用按钮，CLI 启用入口保留。真实浏览器回归通过并检查桌面、窄屏截图；状态文字在窄屏自动换行。
- `internal/system` 测试程序的 macOS / Linux / Windows arm64 交叉编译，以及 `internal/codex` 的 macOS / Linux arm64 交叉编译通过；用 `go list` 核对 Windows、macOS、Linux、Android、iOS 的编译文件集合。iOS 显式排除 macOS 桌面实现。
- 扩大运行 `internal/app` / `internal/host` 时，4 个测试在 Windows 清理临时 `devices.json` / `devices.db` / `registry.json` 时因文件占用失败。在独立导出的未修改 HEAD 上运行同样 4 个测试，复现相同失败。此次未修改该问题。

Windows 当前完成安装和运行检测，未完成 App 共享后端接入。Flutter 兼容界面的按钮判断已同步，但本轮未运行 Flutter 检查，也未构建完整桌面安装包。

## 此前 macOS 验收

2026-11-11 至 2026-11-12，macOS arm64，Go 1.27.3，Flutter 3.41.6 / Dart 3.11.4，Xcode 27.2 SDK。

| 检查 | 结果 | 范围 |
| --- | --- | --- |
| `go test -race ./internal/...` | 通过 | 授权、路由隔离、撤销、缓存、游标、去重、传输、Codex 协议假服务 |
| `go vet` 核心与新命令 | 通过 | Go 静态检查 |
| `flutter analyze` | 通过 | 无问题 |
| `flutter test` | 通过 | 加密向量、设备 / 会话界面、手机弹窗尺寸与键盘收起；在线测试单独运行 |
| `scripts/test-device-browser.py` | 通过 | Chrome 与 Go 主机实际加密配对、WebRTC 直连、增量、重复 command ID、IndexedDB 重载 / 清理 |
| `scripts/test-flutter-relay.py` | 通过 | Flutter VM 加密中继、Chrome WebRTC、大数据分片、条件缓存 |
| 原生桌面操作 | 通过 | 打开 App → Go FFI → 输入模拟 ID / 密码 → P2P → 原会话发送 → 收到 echo |
| macOS 桌面单实例 | 通过 | 正式包 / Flutter runner 连续重复启动 4 次，原桌面 PID 不变，仅一个桌面应用 |
| macOS App 正常退出接入 | 通过 | 临时 AppleScript 测试 App（路径包含空格）验证按路径发送 quit 成功；拒绝 quit 时保留运行实例，解除拒绝后仍可正常退出；修复 Finder alias 导致的 -1728，不重启真实 Codex App |
| 会话来源与目录（此前版本） | 通过 | Go 来源归一化 / 分页、Flutter 目录排序 / 折叠 / 原路由、Chrome 同套界面测试；本机 277 个真实会话分类为 App 42、VS Code 58、CLI 37、子 Agent 12、旧 IDE/App 127、其它 1；61 个工作目录，全量 277 后增量 0；本轮已移除来源筛选 |
| 项目聚合会话栏（本轮） | 通过 | App / CLI / Claude 假数据跨来源聚合、重复 Codex 线程去重、已知 VS Code 隐藏、保留控制路由、目录折叠与最近更新时间排序；Flutter VM 与 Chrome |
| 虚拟聊天列表（本轮） | 通过 | 1500 轮 / 3000 条正文消息仅构建视口附近；初始底部、导航跳转、追加消息 / 历史分页保持阅读位置、360px 手机布局；VM 与 Chrome |
| 消息与输入（本轮） | 通过 | reasoning / tool / commentary 隐藏、生成 Thinking、Enter 发送、Shift+Enter 换行、中文输入法不误发送；VM 与 Chrome |
| 临时执行活动 | 通过 | Go 假快照验证 commandActions 分类、各工具独立状态 / 耗时、缺失状态不继承 turn；Flutter VM / Chrome 验证初始 Thinking、工具执行默认折叠、明细展开 / 耗时、同轮轮询保留状态、完成后 Thinking、整轮完成 / 失败 / 中断清除、新一轮收起、审批仍可操作；1000 项活动明细按需构建。新版核心已随单进程桌面更新 |
| 模型切换（本轮） | 通过 | Go 模拟 WebSocket 验证目录分页 / 隐藏模型过滤 / turn-start 模型参数；Flutter 目录选择与发送路由贯通。当前运行中的旧后台未强制重启，新接口需加载新版后台 |
| Agent 接入状态 | 通过 | 无独立远控开关；假 CLI 验证被动发现、自动附加仅做握手、显式启用可启动后台；真实本机 CLI daemon 自动附加后 ready，App 保持待接入 |
| 扫码平台范围 | 通过 | Flutter 六个平台变体与 Chrome 测试；仅 iOS / Android 原生显示扫码，Web 与桌面隐藏，macOS 包已移除摄像头用途声明 |
| macOS Release | 通过 | 宿主架构，打包 Go dylib；新版不含 agenthubd。构建默认选择稳定的 Apple 证书签名，先退出 Hub 再更新固定路径，旧包备份为 ZIP；显式 ad hoc 开发构建不保证复用隐私授权 |
| Web Release | 通过 | JS 构建，浏览器业务核心不使用 Go Wasm |
| Android Release APK | 通过 | arm64-v8a / x86_64，使用调试签名，非商店发布签名 |
| LAN 测试构建与服务 | 通过 | 服务监听 `0.0.0.0:7443`，回环与 `10.0.4.191:7443` 健康检查通过；Android / macOS / Web 统一内置 `AGENTHUB_SERVICE=http://10.0.4.191:7443`；Android manifest HTTP 许可、编译后 Go LAN 策略、双 ABI 和 APK 签名检查通过，HTTP 下载哈希与产物一致；未连接安卓真机 |
| 进程内 Go 运行时 | 通过 | Go race 验证直接 Provider 调用、并发请求、退出取消、重开保留身份 / 密码；Python 实际调用 C ABI，bootstrap PID 等于调用者，旧控制端口被占用仍可运行，无 daemon.pid，显式退出后拒绝新请求 |
| LAN HTTP 策略 | 通过 | Go race / vet、Dart 默认及显式 LAN 构建测试：回环可用、RFC1918 需显式启用、公网 HTTP / WS 仍拒绝，二维码仅替换同协议同端口的回环服务地址 |
| iOS Release App | 通过 | device arm64，无签名；Go XCFramework 同时有 arm64 simulator slice |

自动化与桌面发送验收全部针对模拟 Agent，未向用户真实 Codex 会话发送测试消息。没有为测试重启用户的 Codex App。现有 daemon / App 的桌面入口依赖版本；真实双端并发、手机扫码、真机 FFI、不同运营商 NAT、后台恢复、Windows / Linux 和 Docker 部署尚未在本轮完成运行验收。

macOS 当前 Xcode 的 `lipo -verify_arch` 与 Flutter 双架构命令存在兼容问题；Release 配置固定 `NATIVE_ARCH_ACTUAL`，Go 桥接与之匹配。需要 Intel 版本时在 Intel 主机构建，不能将本地产物标记为 Universal。Android Pion 的 anet 依赖要求链接 `-checklinkname=0`，按该库 README 限定在 Android 构建脚本中设置。

2026-11-12 单进程迁移：用户授权退出旧 helper 后，两个历史 `agenthubd` 均已退出；新版桌面进程加载 Go dylib 并直接打开 host.db / client.db，无旧本机 HTTP 控制监听和 daemon.pid。设备 ID `788406301242` 与 1 个已保存授权保持；服务地址按当前网卡 IP 更新为 `http://10.0.4.140:7443`。官方 App 已重新连接原端口的 Provider WS 端点，Codex app-server 的父进程为桌面 App。重复启动验收保持同一桌面 PID。菜单栏隐藏 / 完整退出入口已构建，尚未对真实 Codex 会话进行退出破坏性测试。

官方后端双直连回归：删除 Hub WS 网关；隔离模拟 App 直接连官方协议 fixture，启动它的 Hub 测试进程完整退出后仍可继续 RPC，新 Hub 复用相同 PID / 端点，关闭新 Hub 连接仍不影响 App。启动身份检查覆盖手动启动、未记录、回收 PID、断连；runtime 记录拒绝非本机端点和错误进程代际。自动化不启动真实 Agent 或恢复真实线程。

Web 同源地址回归：VM / Chrome 验证连接表单在 Web 隐藏服务地址字段，API 参数使用当前页面 origin；Chrome 特意传入错误的 `AGENTHUB_SERVICE` 构建变量，仍使用页面地址。原生端保留配置能力。

2026-11-13 实时输出与网络方案更新：

- 工具已结束、本轮仍在生成时，尾部汇总继续从左向右流光；完成、授权等待或离线后停止。像素渲染与汇总状态回归通过。
- Codex 同会话订阅将文本增量、公开摘要与工具状态规范化为事件。UTF-16 偏移去重、游标过期、后端连接替换和取消订阅读均有覆盖。测试未向真实 Agent 发送指令。
- Go 全量测试、相关包 race 检查、Flutter VM 66 项和 Chrome 实际配对/直连/附件/离线缓存专项通过。Linux amd64 服务编译通过。
- 自建 STUN Binding 与配置下发已接入两端；模拟公共地址不可达时，自建 STUN 仍提供 srflx 候选。本机服务的真实 UDP 探测通过。复杂公网 NAT 与移动真机网络尚未实测。
- 默认部署端口为 TCP 26330、UDP 26331，无 TURN/DERP/Tailcat 或 UDP 中继端口段。本机保留原测试地址 `http://10.0.4.140:7443` 与设备注册数据，新增 UDP 26331，避免改变已有配对的服务命名空间。

2026-11-13 公网发布包更新：

- 后续发布默认端口收敛为 TCP / UDP 同号 26330；本地既有测试进程不因此重启。临时隔离实例实测同号 HTTPS 与 STUN 可同时监听。
- Linux amd64 / arm64 压缩包包含静态服务、Web、启动脚本、Nginx HTTPS/WSS 示例与校验清单；架构、逐文件 SHA256 和外层压缩包校验通过，不含开发机注册数据或证书。
- macOS arm64 与 Android 双 ABI 公网包默认 `https://ras.lihuipeng.love`，由 Nginx 终止 TLS 时不带后端端口；已保存的服务地址不强制覆盖。macOS 稳定证书签名与压缩包检查通过；Android APK 签名、双 ABI Go 库与禁止明文的 manifest 检查通过，仍使用测试签名，未做安卓真机验收。
- desktop_drop 从 0.8.0 升至兼容当前 Flutter 的 0.8.4，修复 Android Gradle Kotlin 插件缺失导致的构建失败。Flutter VM 66 项通过、1 项浏览器专项跳过；相关 Go 网络、设备、授权与中继测试通过。
- 用户授权服务器部署，但电脑操作工具明确禁止访问 Tabby，未进入远端终端，未上传文件或修改服务器。外部 HTTPS 探测 TLS 握手失败，公网部署未完成验收。

## Native TCP P2P (2026-11-13)

- TCP STUN Binding is now implemented alongside UDP STUN. Application HTTP/WS is 26331/TCP; both STUN transports use 26330. TLS is optional, and Web hosting is optional.
- Native Go core performs reusable-port mapping probes, simultaneous peer dials, fresh session-key mutual authentication, single-channel selection, AES-GCM message framing, heartbeats and relay fallback. Browser clients retain WebRTC / WS(WSS).
- Root `go test ./...`, NSGo `go test ./...`, network-package race tests and targeted `go vet` passed. TCP-only remote integration covers large history, live deltas, concurrent mutations, encrypted heartbeat and connection-loss fallback; no real Agent is used.
- IPv4 and IPv6 TCP STUN/peer source-port reuse tests passed. Windows, macOS, Linux, Android arm64 and iOS builds succeeded; Android uses debug signing and iOS is unsigned. Cross-network physical-device validation remains outstanding.
- Disposable Linux Docker network-namespace test: independent clients behind independent SNAT routers, no inbound forwarding, WebRTC disabled. Three consecutive TCP P2P sessions each transferred 1 MiB; explicit peer TCP blocking retained encrypted relay service and transferred 1 MiB. NATs that actively reset unmatched SYNs can reduce success; bounded randomized retries are used and relay remains available.
- Rebuilt Linux amd64 and arm64 NSGo service bundles were both actually run in Docker. File checksums, HTTP health, Web/WASM, network capability discovery, TCP STUN and UDP STUN all passed. No existing local service, registry or Codex backend was replaced or restarted for these checks.
- Artifacts: `clients/nsgo/dist/server-releases/ras-server-0.1.0-nsgo.tcp.20261113-linux-{amd64,arm64}.tar.gz`; native clients: `clients/nsgo/dist/tcp-clients/`. Deployment guide: `clients/nsgo/deploy/service/README.md`; design and reproducible NAT test: `docs/tcp-p2p.md`.

## RAS branding and embedded Web (2026-11-13)

- New command and release executable: `ras-server`. NSGo app title, native package names, Web manifest/title, pairing display name and user-facing guidance now use RAS. Existing technical application IDs, storage locations and wire protocol identifiers are retained for compatibility; legacy Flutter UI is untouched.
- Release builds stage a freshly built NSGo Web into `internal/webui/assets` in a temporary source module and compile it with Go embed. The working checkout and running LAN Web are not replaced. Packages contain no external Web directory and no start.sh. Default `./ras-server` enables embedded Web; `--no-web` disables it; `--web DIR` is a development override.
- systemd directly invokes `/opt/ras/ras-server --state /var/lib/ras/devices.json`. Network ports remain application 26331/TCP and STUN 26330/TCP+UDP.
- Root and NSGo full Go tests passed. Embedded-file HTTP tests cover WASM MIME, byte ranges, entry-point cache refresh and failure to read runtime data.
- Both packaged Linux architectures were actually executed in disposable Docker containers with only the binary mounted, an empty writable working directory, no arguments and no external Web files. RAS HTML/title/manifest, complete embedded WASM SHA256, health, generated data file, network policy, TCP+UDP Binding and `--no-web` behavior passed.
- RAS native clients built for macOS arm64, Windows x64, Linux x64, Android arm64 and iOS. Android debug signing and unsigned iOS limitations remain. macOS CFBundleName verified as RAS.
- Service packages: `clients/nsgo/dist/server-releases/ras-server-0.1.0-ras.embedded.20261113-linux-{amd64,arm64}.tar.gz`. Clients: `clients/nsgo/dist/ras-clients/`.
