# Standalone ReignHub server

`reignhub-server` provides the device directory, encrypted pairing, signaling, encrypted QUIC, TLS/TCP and WebSocket relay, Web console and TCP/UDP STUN discovery in one pure-Go binary. It uses local SQLite and needs no Cloudflare account or external database. All features start by default. The server never runs Agents or stores chat plaintext; Agents continue running on controlled computers.

## One-command Docker deployment

From the repository root:

```sh
docker compose -f deploy/service/compose.yaml up -d --build
```

Compose mounts `./config.json` at `/config.json` in the container. Edit the file beside `compose.yaml` and restart the service after changes. The default relative state path resolves to `/data/reignhub.db` in the persistent volume. The image also includes this config for standalone Docker runs.

HTTP listens on 26330/TCP. Expose discovery on 26331/UDP and TCP and native relay on 26332/UDP and TCP. Compose's `REIGNHUB_HTTP_PORT`, `REIGNHUB_TCP_PORT`, `REIGNHUB_UDP_PORT` and `REIGNHUB_NATIVE_PORT` change the **published Docker ports**. Set `REIGNHUB_NATIVE_UDP_PORT` and `REIGNHUB_NATIVE_TCP_PORT` when the public relay ports differ. Behind NAT or an HTTP proxy, set `selfStun`, `tcpStunPublic` and `nativeRelayPublic` in the JSON to directly reachable advertised endpoints. Relay clients receive the public pin and connection ticket over signaling. Use HTTPS/WSS for public signaling.

After the first server start, print the native relay's public pin from its existing SQLite state. The command works while the server is running or stopped. Use the same state path or config file as the server. Standard output contains one unpadded base64url Ed25519 public key.

```sh
./reignhub-server --print-relay-pin
# With the supplied Docker Compose deployment, from the repository root
docker compose -f deploy/service/compose.yaml exec -T reignhub-server /reignhub-server --print-relay-pin
```

## Add an external node

On the external Go node, edit `config.json` and start `./reignhub-server`. This example enables both discovery protocols and binds the native relay to its control service.

```json
{
  "http": false,
  "state": "data/relay.db",
  "relayControl": "https://control.example"
}
```

Read its public pin with `./reignhub-server --print-relay-pin`. Register its address and pin with the control service. See the [Go and Worker configuration examples](../../docs/platform/self-hosting.md#add-external-native-relays-and-stun-servers).

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

The server loads `config.json` from its working directory. Release archives include a ready-to-edit file. For a binary-only install, run `./reignhub-server --init` once. It creates the template exclusively and exits. To use another file, supply `--config PATH` to startup, initialization or the PIN command. File settings such as `state`, `web`, `tlsCert` and `tlsKey` resolve relative to the configuration file's directory. Keep the existing SQLite path when upgrading.

Edit service switches and addresses in the JSON, then restart. Omitted fields take their built-in defaults. An unknown field or an invalid service combination reports a startup error. Use `--help` for management actions and `--version` for build identification.

| Deployment | Fields to set in config.json |
| --- | --- |
| Full service with alternate HTTP port | `"listen": "0.0.0.0:8080"` |
| TCP and UDP discovery only | `"http": false` |
| UDP discovery only on port 3478 | `"http": false, "tcpStun": false, "stunListen": ":3478"` |
| Directory with discovery, without signaling, relay or console | `"signal": false, "relay": false, "webEnabled": false` |
| External relay without STUN | `"http": false, "stun": false, "tcpStun": false, "relayControl": "https://control.example"` |

Use `externalStun` (an array of `host:port`, `udp:host:port` or `tcp:host:port` strings) to append discovery nodes, and `externalRelays` to register native relay addresses and pins. For a Worker deployment, publish its discovery nodes through the Worker variable `EXTERNAL_STUN`, for example `["203.0.113.10:26331"]`.

## HTTPS and service management

Use the Nginx or Caddy example to terminate HTTPS/WSS, or set `tlsCert` and `tlsKey` in the JSON. Preserve WebSocket upgrades and the original Host. HTTP reverse proxies must not forward STUN. When DNS is proxied through Cloudflare, use a separate **DNS only** hostname for discovery. For TLS behind a proxy, listen on loopback/private interfaces and expose the proxy publicly.

For systemd, extract under `/opt/reignhub`, create `/etc/reignhub/config.json` containing at least `{"state":"/var/lib/reignhub/reignhub.db"}`, install `reignhub-server.service` to `/etc/systemd/system/`, then run:

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
# Set "web" in config.json to the built console directory.
go run ./cmd/reignhub-server
```

A source-only `go install github.com/monkeyWie/reignhub/cmd/reignhub-server@VERSION` works for API/probes, but Web assets are embedded only by the release builder; set `web` in the JSON or disable Web for such builds. Compilation uses `CGO_ENABLED=0`, including SQLite; no native SQLite library is needed.
