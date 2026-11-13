# CI 与 Release

GitHub Actions 工作流：[CI and Release](../.github/workflows/ci.yml)。只构建维护中的 NSGo / Go 实现；Flutter 和 Worker 历史原型不参与发布。

## 触发

- 提交到 `main`、发起或更新目标为 `main` 的 PR：跑测试、构建和上传临时产物。
- Actions 页面手动运行：执行同样的验证，不创建版本 tag 或 Release。
- 推送 `vX.Y.Z` 或 `vX.Y.Z-rc.N` tag：全部验证成功后发布 GitHub Release。带 `-` 的版本标为 prerelease。

非 tag 构建版本为 `nsgo.yaml` 的版本 + `-dev.<run_number>`；正式版本取 tag，Apple build number / Android versionCode 使用 workflow run number。重打包 Android 原生二维码组件时也传递相同版本，不回退到配置文件中的旧版本。

## 验证与产物

Linux 和 macOS 分别运行两个 Go module 的 `go vet`、`go test -race` 与覆盖率。额外检查 Go 格式、构建脚本、版本解析、发布清单与校验和逻辑。

| 产物 | 格式 |
| --- | --- |
| macOS arm64 / x64 | `.tar.gz`，内含 `.app` |
| Windows x64 / arm64 | `.zip`，便携目录 |
| Linux x64 / arm64 桌面 | `.tar.gz`，运行需要 GTK 4 |
| Android arm64 | 含原生 QR host 的 APK |
| iOS | `-unsigned.ipa`，需自行签名才能安装 |
| iOS Simulator | 编译验证，不作为设备安装包发布 |
| Web | `.zip` |
| Linux amd64 / arm64 服务 | `.tar.gz`，一个二进制内嵌最新 Web |

PR 和 main 构建的产物在对应 Actions run 的 Artifacts 中，保留 14 天；覆盖率保留 7 天。每次完整构建都会把公开产物合并到 `release-bundle` artifact，验证 11 个预期安装包/归档及各自 `.sha256`，生成总 `SHA256SUMS`；tag 发布直接使用这一份已验证的产物。缺失、额外文件或校验和错误都会阻止发布。

服务复用本次客户端 job 构建的 Web，避免重复构建或混入旧开发产物。Linux amd64 服务会用临时数据和随机本地端口实际启动，检查健康端点、内嵌 WASM/JS、SQLite 与 TCP/UDP STUN 地址下发。

## 配置（均可选）

仓库 Settings → Secrets and variables → Actions：

| 名称 | 类型 | 用途 |
| --- | --- | --- |
| `REIGN_SERVER_URL` | Variable | 原生客户端内置默认服务地址；未配置为 `http://127.0.0.1:26331`。Web 始终使用当前 origin |
| `REIGN_SERVER_DOMAIN` | Variable | 服务包里 Nginx/说明文件使用的域名，默认 `reign.example.com`，不自动配置 DNS/TLS |
| `ANDROID_SIGNING_PEM_BASE64` | Secret | 可选的固定 Android 签名身份，使用 NSGo PEM 格式（RSA private key + certificate） |

没有签名 Secret 也能完成流水线。Android 包名会加 `-debug.apk`，使用本次临时生成的调试身份，无法覆盖安装另一签名的旧包。需要跨版本覆盖安装时，将已有 NSGo `.nsgo/android/debug.pem`（或同格式的专用发布身份）Base64 后保存到上述 Secret，之后持续使用同一身份。不要提交密钥文件；流水线不会把 `.nsgo` 或密钥上传为产物，PR 也不会恢复这个 Secret。这不是 Google Play 上传密钥 / JKS 配置。

macOS 目前是 ad hoc 签名、未公证；iOS 为未签名包。Apple Developer 证书、Provisioning、Notarization 和商店发布尚未接入，不能把 CI 构建成功视为可直接上架。

## 发版

确认目标提交在 main 且 CI 通过后，在本地执行：

```sh
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

测试版本使用例如 `v0.2.0-rc.1`。不需要手动填写所有平台的版本。请根据实际版本选择 tag；上面只是示例，本次 CI 配置不会自动创建第一个产品版本。

工作流仅发布当前 tag 已存在的提交，测试/构建失败不发布。首次发布先创建草稿，文件全部上传后公开；失败后可以在 Actions 重跑，上传使用相同文件名覆盖。不要移动已对外发布的 tag。普通检查只有仓库读取权限，只有 tag 的发布 job 有 `contents: write`。
