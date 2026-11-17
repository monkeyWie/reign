# Standalone ReignHub server

`reignhub-server` provides the device directory, encrypted pairing, signaling, encrypted WebSocket relay, Web console and TCP/UDP STUN discovery in one pure-Go binary. It uses local SQLite and needs no Cloudflare account or external database. All features start by default. The server never runs Agents or stores chat plaintext; Agents continue running on controlled computers.

## One-command Docker deployment

From the repository root:

```sh
docker compose -f deploy/service/compose.yaml up -d --build
```

SQLite lives in the `device-data` volume. HTTP/Web/API listens on 26330/TCP; STUN uses 26331/TCP and 26331/UDP. Set `REIGNHUB_HTTP_PORT`, `REIGNHUB_TCP_PORT`, `REIGNHUB_UDP_PORT` to change published ports. If discovery ports are remapped, also set the advertised endpoints `REIGNHUB_SELF_STUN=stun:public-host:port` and `REIGNHUB_TCP_STUN=public-host:port`. Probe ports must be directly reachable, not passed through an HTTP/CDN proxy.

For a configuration file, mount it read-only (e.g. `./server.json:/config/server.json:ro`) and set the command to `--config=/config/server.json --state=/data/reignhub.db`. Keep the state path in the persistent volume.

## Standalone binary

Extract a release archive for Linux, macOS or Windows (amd64/arm64), verify its `.sha256`, then run:

```sh
./reignhub-server
# Windows: .\reignhub-server.exe
```

The release binary embeds the optimized, chunked WASM console. Visit `/chat/`; `/` redirects there. Both Go and Worker self-hosted packages include only the control console; the official website/docs are published separately. Native clients use the service origin, e.g. `http://192.168.1.10:26330` for LAN testing. Browser cryptography/WebRTC needs HTTPS or localhost; remote/LAN browsers need an HTTPS endpoint. Check `/healthz` and `/p2p/config`.

Unix installation after a server release has been published:

```sh
sh scripts/platform/install-server.sh --version X.Y.Z
# Optional: --prefix /usr/local/bin
```

The installer downloads the matching archive, verifies SHA-256 and installs to `~/.local/bin` by default. It does not configure a background service. Copy the bundled systemd unit for Linux, or use Homebrew services on macOS/Linux.

## Configuration and ports

Settings apply in this order: built-in defaults → JSON configuration → explicit command-line arguments. See `server.json` for the complete default feature switches. Unknown JSON fields fail startup, so typos cannot silently change deployment behavior.

```sh
./reignhub-server --config ./server.json \
  --listen 0.0.0.0:8080 \
  --stun-listen 0.0.0.0:3478 \
  --tcp-stun-listen 0.0.0.0:3479 \
  --state /var/lib/reignhub/reignhub.db
```

TCP and UDP listeners have independent addresses and ports. Disable either with `--stun=false` or `--tcp-stun=false`. `--web-enabled=false` disables Web, `--relay=false` disables relay, and `--signal=false --relay=false` leaves the directory/pairing APIs. With `--http=false`, no HTTP/API/Web/socket service or SQLite database is opened:

```sh
# Probes only, for a Cloudflare Worker deployment
./reignhub-server --http=false --stun-listen :3478 --tcp-stun-listen :3478
```

Publish these addresses in Worker variables `SELF_STUN=stun:probe.example.com:3478` and `TCP_STUN=probe.example.com:3478`. The standalone lightweight `reignhub-probe` remains available as an optional smaller download.

By default the Go server advertises its request hostname and actual probe ports. Behind a CDN, NAT port mapping or reverse proxy, provide direct public probe addresses:

```sh
./reignhub-server --self-stun stun:probe.example.com:3478 \
  --tcp-stun-public probe.example.com:3479
```

Additional flags: `--public-stun URLS` (comma-separated; empty disables public STUN), `--origins https://other-console.example.com` (exact additional browser origins), `--web DIR` (built `assets/chat` directory), `--tls-cert CERT --tls-key KEY`, `--version`, `--help`.

## HTTPS and service management

Use the Nginx or Caddy example to terminate HTTPS/WSS, or pass a TLS certificate/key directly. Preserve WebSocket upgrades and the original Host. HTTP reverse proxies must not forward STUN. When DNS is proxied through Cloudflare, use a separate **DNS only** hostname for discovery. For TLS behind a proxy, listen on loopback/private interfaces and expose the proxy publicly.

For systemd, extract under `/opt/reignhub`, customize `server.json`, install `reignhub-server.service` to `/etc/systemd/system/`, then run:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now reignhub-server
```

The unit uses a dynamic unprivileged user and `/var/lib/reignhub` for state. Stop the service before copying the SQLite database for backups (including WAL/SHM if present); alternatively use SQLite's backup command. Preserve the database when upgrading: it contains service identity/signing keys, token hashes, paired permissions and encrypted pending requests. Replacing it changes the service identity.

## Package managers and building

Release builds produce Homebrew and Scoop definitions with the exact archive URLs and SHA-256 values in `reignhub-server-packaging-<version>.tar.gz`:

```sh
brew tap-new reignhub/local
cp Formula/reignhub-server.rb "$(brew --repository reignhub/local)/Formula/"
brew install reignhub/local/reignhub-server
brew services start reignhub-server
# Windows:
scoop install ./bucket/reignhub-server.json
```

A maintained tap/bucket must be published separately before a short command such as `brew install monkeyWie/tap/reignhub-server` can work. This source change does not publish or register packages. `brew install reignhub-server` would additionally need a Homebrew core listing or an already configured tap.

```sh
# Fresh optimized Web + six server targets + Homebrew/Scoop definitions
python3 scripts/platform/package-server.py --version X.Y.Z
# Server development without embedded Web
 go run ./cmd/reignhub-server --web apps/site/public/assets/chat
```

A source-only `go install github.com/monkeyWie/reignhub/cmd/reignhub-server@VERSION` works for API/probes, but Web assets are embedded only by the release builder; provide `--web` or disable Web for such builds. Compilation uses `CGO_ENABLED=0`, including SQLite; no native SQLite library is needed.
