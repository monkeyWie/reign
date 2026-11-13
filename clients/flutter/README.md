# Flutter 客户端

所有 UI 使用 shadcn_flutter 0.0.53。桌面同时被控 / 远控，手机与 Web 是控制端。入口 `lib/main.dart`；原生 `lib/core/native.dart` 调 Go C ABI（后台 isolate），浏览器 `lib/core/browser.dart` 使用 WebRTC / WebSocket / IndexedDB，不需要 Go Wasm。旧入口保留为 `legacy_client.dart`。

从仓库根目录运行：

```sh
scripts/build-mvp.sh macos
scripts/build-mvp.sh web
scripts/build-mvp.sh android
scripts/build-mvp.sh ios
scripts/run-mvp.sh
```

原生端必须先生成、打包 Go 桥接库；单独 `flutter build` 不会代替此步骤。iOS XCFramework 由本地 Pod 链接并保留 FFI 导出符号。Android 包含 arm64-v8a / x86_64，要求 API 24+；Pion 的 Android 网络依赖按其文档使用 `-checklinkname=0`，不是全局修改工具链。macOS 当前生成与构建主机相同架构的应用；Windows / Linux runner 已生成，尚未完成打包验收。

调试原生端可用 `--dart-define=AGENTHUB_GO_LIB=/absolute/library`；`AGENTHUB_DATA_DIR` 指定独立测试目录，`AGENTHUB_SERVICE` 设置默认设备服务。Web 始终使用页面 origin，忽略 `AGENTHUB_SERVICE`；同一 Go 服务托管 Web、API 和 relay，连接表单不显示服务地址。开发与测试也须访问同源设备服务托管的 Web 产物。

```sh
flutter analyze
flutter test
# 仓库根目录运行，启动纯模拟主机：
python3 scripts/test-device-browser.py
python3 scripts/test-flutter-relay.py
```

浏览器集成测试验证 Go / Dart 配对加密互通、实际 WebRTC 直连、目录增量、命令去重、页面重载后离线缓存和删除设备时清理缓存。Go 测试验证中继回退、主机确认、密码、撤销和禁用 Agent。真机相机、跨运营商 NAT、切网和后台恢复需要单独验收。

浏览器保存配对身份与缓存到 IndexedDB；本机核心使用私有目录 SQLite / JSON，尚未全面接入系统密钥库。扫码只在 Android / iOS 原生启用；桌面和 Web 使用设备 ID。DataChannel 不申请麦克风权限。

桌面端通过 C FFI 在同一 App 进程内运行 Go HostService / ClientService；不启动独立 helper，也不通过本机 HTTP 调用自身。macOS 关窗隐藏到菜单栏，明确退出时调用 `AgentHubShutdown` 清理连接与数据库。
