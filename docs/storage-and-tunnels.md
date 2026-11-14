# 数据与 Cloudflare 备用线路

当前 UI 是 NSGo，业务与连接核心是 Go；Flutter / CF Worker 目录仅保留历史原型。

## 存储

| 位置 | 格式与内容 |
| --- | --- |
| 原生端 `settings.db` | SQLite：服务地址、设备名、界面/连接配置 |
| 原生端 `identity.db` | SQLite：被控设备身份、密码哈希、配对授权 |
| 原生端既有缓存数据库 | SQLite：控制设备、会话、增量游标与附件索引 |
| Web | IndexedDB：身份、设备、消息和附件缓存；不再加载 SQLite WASM |
| 自部署服务 `data/reignhub.db` | GORM + SQLite：设备、令牌哈希、授权、服务身份 |

原生 SQLite 使用纯 Go 驱动，开启 WAL、忙等待、限制文件权限。服务不会保存客户端的原始设备 token 或端到端会话密钥。JSON 旧配置可读入，但新的写入进入 SQLite；不删除旧文件。服务端旧 JSON registry 不做自动迁移，需换新的数据库路径和重新登记。

服务可通过 `pkg/service.WithDatabase(*gorm.DB, origins)` 组合到其它入口；传入 PostgreSQL/MySQL 等 GORM dialector 后仍需对目标数据库进行实际验证。当前在线房间、限流与待审批请求在进程内存中，因此应单实例运行；换数据库本身不会自动获得横向扩展能力。

## 连接顺序

原生端并发尝试 WebRTC/UDP 与原生 TCP；浏览器尝试 WebRTC。公共和自建 STUN 都参与发现候选地址。直连成功后优先发送业务数据。

开启 Cloudflare 时，控制端维护两条独立认证线路：Cloudflare Tunnel 与自己的 WS/WSS 服务。两条线路并行连接，任意一条先成功就可使用；随后有直连则优先直连，没有直连则选 Cloudflare，最后自己的中继。故障线路在后台恢复。指令只向当次选中的连接发送一次，断线或超时不自动重发写操作。进行中的请求可能需要刷新确认结果，再人工重试。

Cloudflare 是额外中继路径，不是 P2P，也不会消除设备目录和配对信令服务。隧道不能解决完全无法访问目录服务的首次配对。所有业务消息继续使用原有端到端认证加密。

## 自部署：Quick Tunnel

在桌面设置中开启“备用云线路（Cloudflare）”。优先用已安装的 `cloudflared`，缺少时下载固定版本官方组件并验证 SHA256。首次安装需要访问 GitHub。不支持的平台仍可使用直连和自建中继。

组件只转发 ReignHub 的随机回环端口，公开的只有加密 `/relay`，不会公开本机文件、设置、明文 Agent API 或目录服务。隧道访问凭据由每台设备的配对密钥独立派生，撤销设备也会断开其隧道连接。

Quick Tunnel 随机域名可能变化，无可用性承诺，适合验证。生产环境可在本机配置固定 Named Tunnel，或使用商业服务的托管隧道接口。连接状态会显示在设置中；隧道失败不会影响原有 P2P 和自建中继。

## 固定域名与托管接入

本机配置字段：`cloudflareEnabled`、`cloudflaredPath`、`cloudflareTokenFile`、`cloudflarePublicUrl`、`cloudflareListen`。Named Tunnel 的 Cloudflare ingress 必须指向对应回环地址，且其余路由返回 404。token 存在权限为 0600 的文件，通过 `--token-file` 传入，不放进命令行参数。

未显式指定 token 时，主机尝试认证调用 `/cloud/tunnel`。自部署入口返回 404，就使用 Quick Tunnel；托管入口可返回 HTTPS origin、connector token 和固定 `127.0.0.1:26332`。认证失败或服务异常不会悄悄降级成无管理的 Quick Tunnel，而是后台重试，同时保留其它线路。

服务只对已配对控制设备提供隧道地址；控制设备绑定目标 ID 和公钥，重新发现地址不能更换设备身份。Cloudflare 运营凭据只属于服务器，不进入公开客户端。

真实 Cloudflare 连通性和容量依赖部署地区、域名和 Cloudflare 的服务条件；本项目没有创建隧道或购买资源作为构建步骤。
