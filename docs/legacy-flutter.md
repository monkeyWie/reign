# Legacy Flutter prototype

Historical implementation notes. The maintained client is NSGo.

# RAS

当前 UI 使用 NSGo，入口与构建见 [NSGo 客户端](clients/nsgo/README.md)，服务部署见 [NSGo 服务包](clients/nsgo/deploy/service/README.md)。发布版执行 `./ras-server` 即可，完整 Web 已内嵌，无需脚本或外部静态目录。新服务应用端口为 26331/TCP，TCP/UDP STUN 均为 26330；支持原生 TCP P2P 和可选 TLS。下文保留旧 Flutter 实现记录，旧版端口和公网 HTTPS 限制不适用于当前 NSGo 版本。

Flutter + [shadcn_flutter](https://sunarya-thito.github.io/shadcn_flutter/) 界面，Go 核心。桌面同时是被控端和控制端；iOS、Android、Web 是控制端。输入设备 ID 或扫码，经主机确认或密码授权后，管理远端 Agent 的真实会话。WebRTC 直连优先，失败时用端到端加密的 WebSocket 中继；Web 不加载 Go Wasm。

当前 MVP 包含设备服务、双角色桌面、独立 Codex App / CLI Provider、多设备连接、授权撤销、SQLite / IndexedDB 缓存与目录全量/增量同步。无账号即可自部署。SaaS 的身份、套餐和同账号配对预留在服务业务层，本期未实现。

新登记的设备使用服务端分配的 6 位数字连接 ID，界面按 `123 456` 显示；分配时检查唯一性。连接 ID 是查找设备的编号，配对仍须主机确认或密码授权。客户端将 ID 作为字符串处理，后续可扩展为字母数字组合。本次不迁移旧设备编号。

## 本地运行

需要 Go 1.27.3+、Flutter 3.41.6 / Dart 3.11.4+。锁定 shadcn_flutter 0.0.53；无需升级全局 Flutter。macOS 需要 Xcode / CocoaPods，最低 macOS 13。

```sh
(cd clients/flutter && flutter pub get)
./scripts/build-mvp.sh macos
./scripts/build-mvp.sh web
./scripts/run-mvp.sh
```

桌面只有一个应用入口 `build/AgentHub.app`，显示名称统一为 Agent Hub，包含本机被控面板与远控会话界面。重复打开会唤起已有窗口，正式包和 Flutter 构建目录也共用单实例保护。桌面直接通过 FFI 调用进程内的 Go 核心，不打包或启动 `agenthubd`，也不经过本机 HTTP 调用自身。启动后打开「服务设置」，填 `http://127.0.0.1:26330`，即可登记设备 ID；Web 客户端访问同一地址。首次连接由被控方确认，或在被控端设置访问密码。默认不启用或重启任何真实 Agent。

本地服务数据存于 `build/local-service/devices.json`；桌面数据存于系统应用支持目录下的 `dev.agenthub.agenthubClient/agenthub`，Go 主机与 Flutter UI 同属一个 App 进程。macOS 关闭窗口会隐藏到菜单栏，远程连接继续；点击菜单栏「退出 Agent Hub」或 Cmd+Q 会通过 C FFI 取消请求、关闭连接并保存数据库，然后退出整个应用。再次打开会恢复已有设备身份和授权。旧版留下的 `agenthubd` 需要在升级时退出，不能与新版并行使用同一数据目录。

macOS 构建会在修改 App 前正常退出运行中的 Hub（不会强退或停止官方 Agent），使用钥匙串中的 Developer ID Application 或 Apple Development 证书签名；可用 `AGENTHUB_CODESIGN_IDENTITY` 指定同一个签名身份。更新后路径和签名身份保持一致，系统才能复用“文稿”等隐私授权。没有证书时须显式设置 `AGENTHUB_CODESIGN_IDENTITY=-` 使用临时签名，此模式更新后可能重新请求授权。旧版本仅备份为 ZIP，不再移动运行中的 App 到 `.bundle` 目录。早期版本启动并脱离 Hub 运行的 Agent 仍可能被 macOS 归属到旧备份；应在任务完成后自行退出并重新接入该 Agent，新 Hub 无法改变已有进程的系统归属。

**手机连接需要可访问的服务地址，不能填电脑的 127.0.0.1。** 正式构建要求 HTTPS；局域网 HTTP 使用下面的显式测试构建。扫码包含服务地址、设备 ID、公钥，不包含长期配对密钥。客户端首次通过设备服务发现公钥；扫码可同时固定主机公钥，后续连接检查已保存的身份。

## 无真实 Agent 的演示

```sh
go build -o build/agenthub-demo ./cmd/agenthub-demo
./build/agenthub-demo --web clients/flutter/build/web
```

终端输出模拟主机的设备 ID、公钥和测试密码 `demo-password`。在桌面或 Web 中连接该 ID，输入测试密码，可查看模拟会话、新建会话、发送 echo 消息；不会读取 Codex 数据、调用模型或执行命令。此演示仅监听本机，与正式设备服务二选一运行。

## Codex 接入

- **Codex App**：启用后启动或复用独立的官方共享 app-server。点击「接入 App」，界面列出当前实例并要求本机确认正常退出、携带 `CODEX_APP_SERVER_WS_URL` 与 `CODEX_APP_SERVER_FORCE_CLI=0` 重新打开；不修改安装包或官方 config.toml。Codex App 与 Hub 直接连接同一个官方 server，不经 Hub 网关；只有 Hub 启动的 App 实例与实际 server 连接均匹配才可远控。手动启动的普通 App 只会被发现，其私有 stdio 后端不能直接附加。Hub 退出只关闭自己的连接，官方 server 和 App 可继续运行；下次打开 Hub 复用记录中的同一 server。若记录中的后端已停止，Hub 会在原本机地址恢复它，并重新验证已接入的 App 进程，让 App 有机会自动重连；原端口被占用时会报错，不会悄悄换地址。
- **Codex CLI**：启用后用 `codex app-server daemon version` 发现正在运行的官方后台，再通过返回的 Unix socket 接入；不会因为检测而启动模型或新建会话。未运行后台时，先在终端启动支持 daemon 的 Codex，再重新启用 Provider。`--no-daemon` 会话不能附加。

两个入口分别显示和缓存，不假设所有历史均属于同一运行实例。会话用原 ID 接续；writer conflict 原样报错，不复制会话。支持会话目录、历史分页、新建、发送、引导、中断和审批。**Codex App 接入目前仅实现 macOS；Windows CLI 的本地传输尚未适配验证。** 官方 App 私有环境变量入口可能随版本变化。

## 手机与 Web 构建

```sh
./scripts/build-mvp.sh android  # Android SDK + NDK 28.2.13676358，生成 arm64 / x86_64 Go 库与 APK
./scripts/build-mvp.sh ios      # Xcode，生成设备 / Apple Silicon 模拟器 XCFramework 与未签名 App
./scripts/build-mvp.sh web      # Flutter JS + 浏览器 WebRTC / WebSocket / IndexedDB
```

Android APK：`clients/flutter/build/app/outputs/flutter-apk/app-release.apk`。iOS：`clients/flutter/build/ios/iphoneos/Runner.app`，真机安装需要配置自己的签名。手机核心走 Go FFI；Web 协议适配走 Dart，UI 完全共用。Android NDK 路径可用 `AGENTHUB_NDK` 覆盖，Flutter 命令可用 `FLUTTER_BIN` 覆盖。

### 内置默认服务地址

桌面和移动端通过构建变量 `AGENTHUB_SERVICE` 设置默认地址。Web 页面、API 和 relay 由同一个服务提供，始终使用当前页面的 origin（协议、主机和端口），不读取内置服务器地址，也不显示服务器地址输入框。正式域名部署示例：

```sh
AGENTHUB_SERVICE=https://hub.example.com ./scripts/build-mvp.sh android
AGENTHUB_SERVICE=https://hub.example.com ./scripts/build-mvp.sh macos
./scripts/build-mvp.sh web  # 同一份 Web 产物可部署到不同域名，无需重编译地址
```

原生端默认地址用于首次连接表单和新桌面主机初始化；用户已有的服务配置和已配对设备不会因升级被覆盖，仍可在界面中手动修改。Web 新连接和重连统一使用当前页面 origin。

局域网测试示例（电脑当前 IP 为 `10.0.4.140`，手机须能访问此网络）：先启动设备服务，再构建客户端。

```sh
go build -o build/agenthub-service ./cmd/agenthub-service
./build/agenthub-service --listen 0.0.0.0:26330 \
  --state build/local-service/devices.json --web clients/flutter/build/web

# 在另一个终端执行；macos 同样使用这两个变量。
# Web 只需 AGENTHUB_ALLOW_LAN_HTTP=true，地址跟随访问页面。
AGENTHUB_SERVICE=http://10.0.4.140:26330 AGENTHUB_ALLOW_LAN_HTTP=true \
  ./scripts/build-mvp.sh android
```

`AGENTHUB_ALLOW_LAN_HTTP=true` 同时开启 Go、Dart 与 Android Release 的局域网 HTTP 测试支持；地址校验仅额外允许 RFC1918 IPv4 字面地址，不允许公网 HTTP。不要只给 Flutter 手动传此 dart-define，否则 Go 策略不会同步启用。正式 HTTPS 构建不设置该变量。LAN 测试版桌面的二维码在相同协议和端口下会把本机回环服务地址替换为构建时指定的 LAN 地址，让手机可以扫码访问；运行中的主机仍可通过回环地址连接同一服务。

## 自部署

运行 `build/agenthub-service --listen 127.0.0.1:26330 --state /private/path/devices.json --web /path/to/flutter/build/web`，用 HTTPS 反向代理暴露 HTTP / WebSocket，Web、API 和 relay 同源部署，无需额外跨域设置。二进制、TLS 与 Docker 部署示例见 [部署说明](deploy/service/README.md)。本仓库没有自动发布官方公有服务。

设备服务负责 ID、在线状态、加密配对信箱与授权后的中继路由。聊天、密码和 WebRTC 信令在客户端与主机间加密；服务不持有聊天密钥。服务内置 UDP STUN（默认 26330），通过 `/v2/network` 同时向被控端和控制端下发公共与自建 STUN 地址。采用 Trickle ICE 尽可能直连；失败统一走自己的加密 WSS 中继，不部署 TURN/DERP，也不引入 Tailcat。直连成功后业务数据走 P2P，信令 WebSocket 保持连接；服务断开会结束关联连接。

## 验证

```sh
go test -race ./internal/...
go vet ./internal/... ./cmd/agenthub-service ./cmd/agenthub-bridge ./cmd/agenthub-demo
cd clients/flutter
flutter analyze
flutter test
cd ../..
python3 scripts/test-embedded-ffi.py
python3 scripts/test-device-browser.py
python3 scripts/test-flutter-relay.py
```

已验证原生桌面、Web release、iOS 无签名构建及 Go / 浏览器配对、直连、加密中继、断线回退、拒绝未授权访问、密码授权、撤销、命令去重及持久缓存。自动化只使用模拟 Agent。手机真机扫码、蜂窝网络切换与后台恢复仍需设备验收。

MVP 的目录同步使用持久化变更游标及删除记录，客户端每 2 秒轮询；打开的 Codex 会话订阅带 epoch/游标的文本与活动增量，历史按页缓存，断线或事件过期后用快照恢复。增量日志为有界内存缓冲，不是永久消息日志。设备凭据与缓存保存在权限受限的本机文件 / 浏览器 IndexedDB，尚未全部迁入系统密钥库；Web 仅应在可信浏览器使用。公开运营前仍需服务配额、审计、身份与套餐层及协议安全评审。

[验收范围](docs/validation.md) · [产品与后续规划](docs/product-v2.md) · [MVP 协议与实现](docs/mvp.md) · [开源 / SaaS 分层](docs/open-source-boundary.md)

旧 MyGo 客户端、`agenthub1:` 共享 token relay 与 CF Worker 位于遗留目录，保留回归参考；新设备协议使用 `agenthub2:`，不能互换。
