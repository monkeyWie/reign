# ReignHub connection service

Current deployment instructions: [NSGo server package](../../clients/nsgo/deploy/service/README.md).

| Port | Purpose |
| --- | --- |
| 26331/TCP | Embedded Web, API, device registration, pairing, signaling, WS/WSS relay |
| 26330/TCP | TCP STUN Binding for native TCP hole punching |
| 26330/UDP | UDP STUN Binding for WebRTC |

HTTP/WS and HTTPS/WSS are supported. A public IP works without certificates. TCP and UDP STUN require direct access or layer-4 forwarding; Nginx HTTP proxying handles only the application port 26331.

```sh
python3 clients/nsgo/scripts/package-service.py --domain ras.lihuipeng.love
# Extract a release, then run the embedded-Web executable:
./reignhub-server
```

Native clients attempt WebRTC/UDP and native TCP in parallel. TCP includes mapping discovery, port reuse, simultaneous open, authenticated connection selection, encrypted framing, heartbeats and relay fallback. Strict NATs can still prevent direct connections. No TURN, DERP or Tailcat is required. See [TCP P2P validation](../../docs/tcp-p2p.md).

`/v2/network` provides UDP `publicStun` / `selfStun` and separate TCP `tcpStun` endpoints. Override external addresses using `--stun-public` and `--tcp-stun-public` when a proxy/CDN cannot forward STUN. Persist the SQLite database (`data/reignhub.db`). GORM provides the database interface. Online routes and approval mailboxes are still in memory; this version runs as one service instance. No external database or Redis is required.

Build the current NSGo package:

```sh
cd clients/nsgo
python3 scripts/package-service.py --domain ras.lihuipeng.love
```

Output: `clients/nsgo/dist/server-releases/`, Linux amd64 and arm64, embedded NSGo Web, Nginx/systemd examples and SHA256 checksums.

The Dockerfile / Compose in this directory retain the legacy Flutter Web build, with ports updated to 26331/TCP and 26330/TCP+UDP. Use the NSGo packaging command above for the current UI. Legacy Flutter browser networking is unchanged.
