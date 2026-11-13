# Cloudflare Workers relay

Agent Hub relay 的 Cloudflare 实现：**Worker + SQLite-backed Durable Objects + WebSocket Hibernation API**。不需要 VPS、KV、D1 或容器，不运行 Go 后台二进制；原生 Go 客户端和端到端加密协议保持兼容。

每个 `hostId` 对应一个 Durable Object。主机与客户端都主动发起 WSS 连接，同一个对象负责转发。路由信息保存在 WebSocket attachment 中，休眠后不依赖原有 JS 内存。不会在 CF 端保存聊天记录或配对密钥。

## 部署

需要 Node.js 22.12+ 和已登录的 Cloudflare 账户。

```sh
cd deploy/cloudflare
npm ci
npx wrangler login
npm run deploy
npx wrangler secret put AGENTHUB_RELAY_TOKEN
```

最后一条命令会提示输入 token。使用自己生成的随机 token（至少 32 字符）；例如另一个终端执行 `openssl rand -hex 32`，将结果保存后输入。secret 配置前 `/relay` 拒绝所有连接。

Wrangler 会创建 `ROOMS` namespace，并执行 `new_sqlite_classes` migration。首次使用可修改 `wrangler.jsonc` 中的 Worker 名称，避免覆盖账户内其它项目。

部署后原生客户端配置：

```text
Relay URL: wss://agenthub-relay.<你的子域>.workers.dev
Relay 凭据: 上一步保存的 token
```

桌面主机点击“保存服务设置”，在“扫码配对”中让 Flutter 手机/Web 扫描或导入配对码。P2P 成功后业务消息直接传输；CF WSS 留作信令与回退。可为 Worker 配置自定义域名，URL 仍使用 WSS。

这是可部署源码和本地验证结果，**没有代替用户部署到真实 CF 账户**。

## Flutter Web 与中继同源部署

先在 `clients/flutter` 执行 `flutter build web --release --no-web-resources-cdn`，再回到本目录：

```sh
npx wrangler deploy --config wrangler.web.jsonc
```

该配置把 Web 静态文件与 Worker 部署在同一个域名，`/relay` 保留给 WebSocket。默认允许同源浏览器作为 client 连接；host 角色仅接受原生端。独立 Web 域名需要设置 `AGENTHUB_ALLOWED_ORIGINS` 为精确 origin 列表，逗号分隔，不使用通配符。

浏览器不能自定义 WebSocket Authorization，故使用子协议 `agenthub.v1, auth.<base64url-token>`，响应仅协商 `agenthub.v1`；长期凭据不放在 URL。原生 Go 客户端仍支持 Bearer。Web 客户端的配对密钥仅保存在页面内存，页面关闭/刷新后不保留。

当前为共享 relay token 的私有实例；多房间路由不等于多租户授权。不要以此版本开放公有匿名 SaaS。公有服务需要先实现 [匿名租户、设备授权和连接票据](../../docs/product-and-tenancy.md)，后续才启用 GitHub / Google 绑定和收费策略。此版本未代替用户上线公有服务。

## 本地验证

```sh
npm ci
npm run check
npm test
npm run build
```

`npm test` 自动启动本机 workerd，再验证：

- 边缘鉴权、非白名单 Origin 拒绝、Upgrade 校验，以及浏览器子协议凭据和原生主机角色限制。
- 主机隔离、客户端不可伪造 peer ID、断线与主机重连。
- 用原有 Go 客户端完成加密握手和 RPC，不使用替代的 JS 模拟客户端验证业务协议。
- 1.8 MB 分片响应、真实 WebRTC P2P、控制帧 Ping/Pong、未变化的历史快照省略传输、内容变化后的缓存刷新，以及关闭 P2P 后的 Worker relay 回退。

测试需要 Go 工具链，固定使用本机端口 18787；token 是公开的测试常量，只在测试子进程内作为绑定设置。测试结束关闭 workerd 并清理临时状态。

手动本地运行：

```sh
cp .dev.vars.example .dev.vars
# 编辑 .dev.vars 的本地 token
npm run dev
```

使用 `ws://127.0.0.1:18787`；公网则必须 WSS。`.dev.vars`、`node_modules` 和本地 Worker 状态已忽略。

## 免费套餐前提

本实现以 **Workers Free** 为部署前提，不要求开通 Workers Paid。Cloudflare 当前允许在 Free 套餐创建 SQLite-backed Durable Objects；超出免费额度时操作失败，不靠自动升级付费保证可用性。本项目不执行套餐升级。

官方当前列出的 DO Free 额度是 100,000 requests/day、13,000 GB-s/day；入站 WebSocket 消息在 compute requests 计费中按 20:1 折算。外层 Worker 也有单独配额。应以账户面板和官方最新额度为准，不能把免费部署理解为无限使用。

为避免重复历史耗掉消息额度，原生远程客户端会携带快照 revision。主机仍重新读取和核对真实状态，但内容没变时只发一个很小的 `notModified` 响应；内容变化才重新传完整快照。写操作不缓存、不重放。客户端缓存最多 16 MiB / 32 项，CF 端不保存这些缓存。

按当前 1.2 秒间隔、4 个只读 RPC、每个请求和未变化的响应各 1 帧估算，一个客户端全天仅走 relay 的稳定空闲轮询约产生 576,000 入站 WS 消息，按官方 20:1 口径约为 28,800 个 DO compute requests。初次历史同步、实时内容变化、大消息分片、多设备及重连会增加消耗；这不是实际账户用量承诺。P2P 成功后的业务消息不经 CF。

## 成本与限制

- 使用 `ctx.acceptWebSocket()`，不使用普通 `ws.accept()`、Worker 计时器或出站连接；满足条件时可进入 hibernation，避免连接仅空闲就一直产生 DO duration 费用。
- 原生客户端每 25 秒发送协议层 Ping，CF runtime 自动响应；不以 JS 定时器或业务消息维持心跳。
- 每主机最多 16 个客户端，单条文本 frame 最多 2 MiB。加密 RPC 继续使用 12 KB 分片与 16 MiB 重组上限。
- Worker 实现应用 relay，不是通用 TURN、DERP 或 QUIC 服务器。普通 Workers 不提供通用公网 UDP 监听。客户端之间的 UDP P2P 无需经过 Worker。
- 协议层 DERP 可以封装在 WebSocket 中；若以后接 Tailcat，需要实现/适配 DERP 协议及其客户端 WebSocket 支持，不能把本项目 `/relay` 地址直接当成 DERP 地址。
- SQLite-backed DO 可用 Workers Free，但免费额度、请求与 duration 计费仍适用，超限会失败。高频 relay 传输会产生 DO 调用；P2P 成功时这些业务消息不经 CF。
- CF 更新、网络切换仍可能断开 WebSocket。后台会重连，客户端目前需点“连接电脑”重建；写操作不会自动重放。
- 本地已验证 workerd 互通和打包；真实 CF 部署、休眠唤醒和公网 NAT 回归仍需部署后验收。

官方依据：

- [DO WebSocket / Hibernation API](https://developers.cloudflare.com/durable-objects/best-practices/websockets/)
- [DO 套餐和计费](https://developers.cloudflare.com/durable-objects/platform/pricing/)
- [Workers 协议支持](https://developers.cloudflare.com/workers/reference/protocols/)
