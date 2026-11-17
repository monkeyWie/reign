# 连接与中继

当前 MVP 使用 [Go 设备与连接服务](../deploy/service/README.md)，一个二进制包含设备登记、配对、信令、加密 WS/WSS 中继和 TCP / UDP STUN。

- 默认 TCP 26330 提供可选 Web、API、WS/WSS，TCP / UDP 26331 提供 STUN，不需要 UDP 中继端口范围。
- 被控端、原生控制端、Web 控制端都从 `/v2/network` 读取公共与自建 STUN 配置。
- Trickle ICE 立即交换候选，尽可能通过局域网、IPv6 或 UDP 打洞直连；公共 STUN 不可达不会阻止尝试自建 STUN。
- 原生端并发尝试 TCP 映射探测、端口复用与双方同时拨号；直连成功后业务数据走 WebRTC 或 TCP，信令连接保持；直连不可用时统一走自己的加密 WS/WSS 中继。
- MVP 不配置 TURN/DERP，不引入 Tailcat；旧配对数据里的 TURN 配置会被忽略。
- 中继可在打洞期间先传业务数据。写操作结果不确定时不会自动重放。
- HTTP/WS 和 HTTPS/WSS 均支持，TLS 可选，公网 Web 推荐 HTTPS。服务内置 TLS 证书参数，也可使用已有反向代理；STUN 必须分别开放 TCP / UDP 或走四层转发。

配对授权与网络选路独立。每台控制设备使用独立凭据和授权关系，服务不持有会话加密密钥。历史 CF Worker / 全局 token relay 原型留在旧部署目录供参考，不是当前 MVP 的部署路径。
