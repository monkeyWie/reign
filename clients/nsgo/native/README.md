# ReignHub 原生能力扩展

按 NSGo 官方公开 API 接入，不使用 WebView、不修改 Flutter，不修改 NSGo 模块缓存。

- 官方导航：https://nsgo.dev/zh/docs/navigation/
- 官方原生组件与宿主通道：https://nsgo.dev/zh/docs/embedding/
- 官方平台服务：https://nsgo.dev/zh/docs/services/

## 共用能力约定

UI 调用 `core.Native{Component: "ras.qr-camera"}` 展示实时相机画面，
通过 `OnEvent("code", string)` 接收已识别的二维码，`error` 接收可读错误。
`active` 属性暂停/恢复，`retry` 命令在无效二维码后恢复识别。
组件销毁、退后台会释放/暂停相机。照片和相机帧不上传，也不保存到磁盘。
扫码只填充连接信息，仍保留已有公钥校验、确认连接与主机授权。

`cx.CallHost("ras.device", nil, callback)` 返回 `{name: string}`。
后续增加系统能力应继续用宿主通道；需要原生 UI 的能力使用独立 `core.Native` 组件。
平台 SDK 细节放在本目录或 `../nativecap`，业务 UI、协议和 Provider 不依赖 SDK。

## Android

`RasActivity` 继承官方 `NsgoActivity`，用公开的
`NsgoView.registerComponent` 和 `NsgoView.handle` 注册原生能力。
Camera2 提供连续预览，ZXing Core 3.5.3 只解码二维码，按 YUV 平面的行/像素步长读取。
用户打开扫一扫时才申请 CAMERA 权限。使用后置摄像头，支持权限拒绝、后台暂停、
前台恢复、无效二维码重试。设备名称优先读系统 device_name，再回退品牌和型号。

使用 `scripts/build.sh android_arm64 -output dist/...` 构建。
脚本先执行 NSGo 标准构建，再使用官方 `nsgo build -embed` 导出的 AAR classes.jar
编译本应用宿主；d8 合并官方运行时、宿主与 ZXing，使用 NSGo 的公开 apk 包重新打包。
Go 可执行文件、数据目录、应用 ID 和 `.nsgo/android/debug.pem` 签名均保持一致，
更新 release APK 和 SHA256。依赖 JDK 17+、Android SDK；ZXing 下载有固定 SHA256。
当前脚本针对开发包签名；配置生产 keystore 时需同步实现宿主打包的生产签名。
不要用未经宿主打包的原始 `go tool nsgo build android...` APK 发布扫码功能。

## iOS

`../nativecap` 用 `nsgo.Component` 注册 UIView + AVFoundation 扫码组件，
使用 `nsgo.HostChannel` 提供设备名。仅此平台的薄 SDK 适配使用 Go/cgo + Objective-C，
其余业务仍为 Go。NSCameraUsageDescription 在 nsgo.yaml 声明。
iOS 系统可能仅返回通用的 iPhone/iPad 名称，不能保证读取用户自定义设备名。
模拟器没有实际后置摄像头，提供明确错误及相册/手动输入入口，真机扫码需真机验证。

NSGo 已提供所需扩展点，因此本实现不需要框架 fork 或上游 PR。
若要简化上游 CLI，可另提通用的 Android 宿主源码/依赖打包配置能力；不把 ReignHub 的扫码业务放进框架。
