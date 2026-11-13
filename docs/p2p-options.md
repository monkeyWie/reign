# P2P 技术选型复核（2026-11-10）

本文保留早期选型记录；最新产品采用 Flutter + Go，Web 直接使用浏览器 WebRTC，无需 Wasm。P2P 与 relay 是独立于设备 ID / 授权的传输能力，不再以全部网络服务必须运行在 CF Worker 为前提。当前方向见 [产品 v2 基准](product-v2.md)。

WebRTC DataChannel 是成熟的通用方案，原生 Go 中通过 Pion 调用 ICE / DTLS / SCTP，不依赖浏览器或 WebView。当前 MVP 选择它，是为了在纯 Go 客户端中获得现成的 NAT traversal 与可靠双向消息，而不是因为原生应用只能用 WebRTC。

本产品以桌面/未来手机原生端为主，应区分协议成熟度、具体 SDK 稳定性和自主部署难度。

| 候选 | 适合之处 | 主要取舍 |
|---|---|---|
| Tailscale / tsnet | 成熟的 WireGuard、网络发现、NAT traversal、DERP 回退；tsnet 可以嵌入 Go 应用 | 有 tailnet 身份与控制面模型，需要设计用户加入和管理方式；不是把任意 WSS 地址填作 DERP 即可 |
| Tailcat | Tailscale 官方开源，Go library；复用 magicsock / WireGuard / DERP，不要求 Tailscale 账户，也不需要 root 或修改系统路由 | 项目较新，官方明确不承诺 Go API、CLI 或 wire format 稳定；公共 relay 也无 SLA，须固定版本与实测 |
| Iroh | 面向原生应用的 QUIC、打洞、relay、密钥身份；Rust 核心和 Swift/Kotlin 等官方绑定适合移动端 | Go 为社区 FFI 绑定，官方表列平台覆盖明显有限；当前 MyGo Go 客户端引入 Rust / FFI 需要额外维护 |
| go-libp2p | Go 原生，QUIC/TCP、Circuit Relay、DCUtR，网络协议组件完整 | 需配置 relay、发现、资源限制等；对小型 Agent RPC 产品集成面更大 |
| Pion WebRTC | 当前已集成；原生 Go；与 WSS 信令、应用 relay 能清晰分离 | ICE / SDP / DataChannel 状态要管理；当前代码的重连、切网、移动后台恢复尚需产品级完善 |

## 建议

- 现在需要“Go 原生 + 无账户配对 + CF Worker 承载全部应用信令/兜底 relay”：先采用已验证的 **Pion + Worker/DO**，不用等待另一套 relay 协议适配。
- 产品以原生端为主，愿意投入下一轮网络引擎验证：**优先验证 Tailcat**，比较切网恢复、对称 NAT、移动后台和包体积；不能把“复用成熟 Tailscale 内核”直接等同“新 Tailcat SDK 已经有稳定契约”。
- 如果账户与 tailnet 管理可接受，**tsnet** 的完整方案应优先列入稳定性选择。
- 如果未来接 Rust 核心或主要开发 Swift/Kotlin 手机端，**Iroh** 值得重新评估；当前 Go 绑定的可用性不能按 Rust 核心成熟度推断。

本轮没有替换现有 P2P 引擎，也没有声称某库在本产品的 NAT 成功率更高。没有同环境实测数据，不给穿透成功率排名。

## CF Worker 与这些方案的关系

CF Worker 擅长 HTTP/WebSocket 的控制面和应用层中继。它不能直接托管现有 Go ELF/macOS 可执行程序；本项目把 relay 协议移植成 TypeScript + Durable Objects。

普通 Workers 暂不提供通用公网 UDP 监听，不能直接把 coturn 的 UDP 服务或原生 UDP relay 放进去。CF 的 HTTP/3 支持也不代表应用能监听任意 QUIC ALPN。

DERP 本身的中继主要走 TCP/HTTPS，并有 WebSocket 变体，不能把它误认为一定要求 UDP。当前 Tailscale 源码的 native WebSocket client 入口有构建标签/调试开关约束；如果要把 Tailcat 的 DERP 也完全放进 Worker，需要单独验证适配。这是另一种 relay 协议，当前 Worker 不是 DERP 服务器。

## 来源

- [Tailscale tsnet](https://tailscale.com/docs/features/tsnet)
- [Tailcat 官方仓库、Go 示例与 Stability 说明](https://github.com/tailscale/tailcat)
- [Tailcat 官方介绍](https://tailscale.com/blog/tailcat)
- [Iroh 语言/平台支持，Go 为 Community FFI](https://docs.iroh.computer/languages)
- [libp2p DCUtR](https://libp2p.io/docs/dcutr/)
- [Tailscale DERP WebSocket client 源码](https://github.com/tailscale/tailscale/blob/main/derp/derphttp/websocket.go)
- [CF Workers 支持协议](https://developers.cloudflare.com/workers/reference/protocols/)
