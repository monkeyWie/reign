# 原生 TCP P2P

原生端复用现有 Go 核心。WebRTC/UDP 与 TCP 并发探测，业务在探测期间可通过自有 WS/WSS 中继传输。可用时优先 WebRTC，其次 TCP；原生端的 TCP 不是经服务器转发的连接。浏览器不使用原始 TCP socket，继续使用浏览器 WebRTC 与 WS/WSS。

## 链路

1. 服务 `26330/TCP` 提供登记、配对、信令、中继和可选 Web；`26331/TCP + UDP` 分别提供 TCP 与 UDP STUN Binding。
2. `/v2/network` 的 `tcpStun` 是独立的 `host:port` 列表。不能复用 UDP 的映射，更不能把 STUN TCP 探测误当成已经建立 TCP P2P。
3. 原生端按 IPv4/IPv6 建立可复用源端口的 TCP listener，用同一端口连接 TCP STUN，并保留探测连接。局域网/全局 IPv6 的 host 候选与探测出的 srflx 候选通过加密信令交换。
4. 双方保持监听，并从该源端口同时拨号对方候选。使用有界随机重试处理 SYN 时序；搜索最长 25 秒。不做端口预测或大范围扫描。
5. 双向随机挑战与 HMAC 使用当前配对会话派生密钥验证身份，绑定客户端/主机角色。控制端在多个交叉连接中选择一个，两端确认，避免各选不同连接。外部端口开放不意味着第三方可以跳过配对操作 Agent。
6. 消息沿用现有 AES-GCM 加密分块、方向绑定、重放检查与请求 ID。TCP 字节流增加有界长度前缀，使用完整读写，支持大消息。20 秒加密心跳、60 秒读超时、8 秒写超时检测断线。
7. TCP 断开后后续请求使用其他可用直连或中继。发送失败而结果不确定时不会自动重放操作。信令仍保持连接，以维持现有授权和连接生命周期。

服务只做映射探测，不分配 TCP/UDP 中继端口段。STUN 标准支持 UDP、TCP 和 TLS；本次实现为 UDP 和明文 TCP Binding，TLS 证书仍用于可选的 HTTPS/WSS 应用入口，不声称支持 STUNS。

## 验证

```sh
go test ./...
go test -race ./internal/connectivity ./internal/remote ./internal/host ./internal/deviceservice
```

覆盖 TCP STUN 的拆包/粘包、持久探测、源端口复用、畸形长度、服务关闭清理、错误配对密钥/角色拒绝；强制关闭 WebRTC 后验证 TCP 身份校验、会话快照、大消息、增量 watch、并发指令以及 TCP 断开后的中继回退。测试使用隔离 Provider，不操作用户真实 Agent。

真实 Linux NAT 测试使用临时特权 Docker 容器，不修改宿主机网络：

```sh
mkdir -p /tmp/ras-tcp-nat-validation
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -o /tmp/ras-tcp-nat-validation/tcpnat ./internal/remote/testdata/tcpnat
cp scripts/test-tcp-nat.sh /tmp/ras-tcp-nat-validation/test.sh
docker run --rm --privileged \
  --mount type=bind,src=/tmp/ras-tcp-nat-validation,dst=/test,readonly \
  golang:1.26.2-alpine sh /test/test.sh
```

Docker 为 x86_64 时把构建 `GOARCH` 改为 `amd64`。测试建立两个独立客户端命名空间和两个独立 SNAT 路由器，只有出站与连接跟踪的返回包可以通过；不设置入站端口映射，丢弃未匹配的 WAN 入站。禁用 WebRTC，重复验证 TCP P2P 和 1 MiB 消息，然后显式封禁两端 TCP P2P，验证相同应用仍可经中继收取 1 MiB 消息。

## 边界

成功打洞仍取决于 NAT 的 TCP 状态机、映射与过滤策略。主动 Reset、不同目的地址使用不同外部端口、企业防火墙、移动网络切换和系统休眠都可能使本次尝试失败；必须保留中继。测试不能代表所有公网网络。Windows、Android、iOS 已构建验证，尚无各平台跨运营商真机打洞成功率数据。

HTTP 公网 IP 部署和 HTTPS 域名部署都支持。HTTP 网页本身没有来源防篡改保护，公网 Web 推荐 HTTPS；这不是连接的强制条件。原生平台不依赖浏览器 WebRTC 安全上下文或 WebAssembly。
