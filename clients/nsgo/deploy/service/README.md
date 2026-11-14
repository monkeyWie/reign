# ReignHub 服务

一个 `reignhub-server` 二进制内嵌完整 NSGo Web，同时提供设备登记、ID、配对、信令、加密 WS/WSS 中继、UDP STUN 和 TCP STUN。部署无需另带 Web 目录，也不需要 Go、Node、Flutter、NSGo 或启动脚本。

## 直接启动

按服务器架构选择 Linux amd64 或 arm64 压缩包，解压并校验 `sha256sum -c SHA256SUMS`，然后运行：

```sh
./reignhub-server
```

访问 `http://公网IP:26331` 即可打开 ReignHub Web。桌面和手机客户端填写相同服务地址。TLS 和域名都是可选项；运行数据自动保存到当前目录的 `data/reignhub.db`，需持久化并备份。

只上传 `reignhub-server` 也能正常提供页面和服务。压缩包中的说明、Nginx 和 systemd 示例都不是运行依赖。

## 端口

| 端口 | 用途 |
| --- | --- |
| 26331/TCP | 内嵌 Web、API、设备登记、配对、信令、WS 中继 |
| 26330/UDP | UDP STUN Binding，辅助 WebRTC 打洞 |
| 26330/TCP | TCP STUN Binding，辅助原生 TCP 打洞 |
| 443/TCP（可选） | 已有 Nginx 的 HTTPS / WSS 入口 |

26330 的 TCP 和 UDP 都应直接放行，不能当 HTTP 请求反代。探测地址由 `/v2/network` 自动下发；经过不支持 STUN 的 CDN 或端口映射时，指定可直达地址：

```sh
./reignhub-server --stun-public stun:公网IP:26330 --tcp-stun-public 公网IP:26330
```

客户端并发尝试 WebRTC/UDP 和原生 TCP，直连失败时走自己的 WS/WSS 中继，无 TURN、DERP 或 Tailcat。TCP 路径包括源端口复用、映射探测、双方同时拨号、配对会话身份验证、消息加密和心跳。严格 NAT / 防火墙仍可能使打洞失败，保留中继是必要的。浏览器使用自身 WebRTC 能力，不支持时保留中继。

## 可选 HTTPS / Nginx

域名 `@DOMAIN@`。将 `nginx.conf.example` 合并进现有配置，修改证书路径，`nginx -t` 后 reload。Nginx 反代 `127.0.0.1:26331`，支持 WS Upgrade 与 WASM gzip：

```sh
./reignhub-server --listen 127.0.0.1:26331 --origins https://@DOMAIN@
```

客户端填写 `https://@DOMAIN@`。TCP/UDP 26330 仍直接开放。证书由已有证书服务或部署者管理，程序不自动申请。

让 Go 直接处理 TLS 也可以：

```sh
./reignhub-server --tls-cert /path/fullchain.pem --tls-key /path/privkey.pem
```

此时客户端填写 `https://@DOMAIN@:26331`。HTTP/WS 与 HTTPS/WSS 都支持；公网 Web 推荐 HTTPS，因为 HTTP 网页下载本身没有防篡改保护，应用层消息加密不能替代网页来源真实性。

## 可选参数

- `--state /path/reignhub.db`：指定 SQLite 数据库。
- `--no-web`：关闭内嵌 Web，保留 API、配对、信令、中继及探测。
- `--web /path/to/web`：开发时用外部静态目录覆盖内嵌 Web；正式部署无需此参数。
- `--public-stun ''`：只使用自建 UDP STUN。
- `--tcp-stun-public host:port,host2:port`：额外或独立 TCP STUN 地址。

包内 `reignhub-server.service` 直接执行 `/opt/reignhub/reignhub-server --state /var/lib/reignhub/reignhub.db`，不依赖脚本。默认内嵌 SQLite，通过 GORM 访问，无需外部数据库或 Redis。当前为单实例服务：在线连接路由、限流和待审批请求在内存中，暂不支持多实例负载均衡。包中没有开发机身份、密钥或运行数据。

## 检查

```sh
curl --fail http://公网IP:26331/healthz
curl --fail -H 'Content-Type: application/json' -d '{}' http://公网IP:26331/v2/network
```

网络配置包含 UDP 的 `selfStun` 与独立 TCP 的 `tcpStun`。新原生客户端必须升级才能使用 TCP 路径。

发布命令 `python3 clients/nsgo/scripts/package-service.py --domain @DOMAIN@` 会在临时目录构建最新 ReignHub Web，然后通过 Go embed 编进二进制，不覆盖本地正在提供的旧 Web。直接 `go build ./cmd/reignhub-server` 的开发构建只有占位资源：使用 `--web DIR`，或用发布命令生成完整内嵌版本。
