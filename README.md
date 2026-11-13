# Reign

**Every agent. Under control.**

Reign lets you access coding agents on your own computers from desktop, Android, iOS and the Web. The maintained client uses [NSGo](https://nsgo.dev/zh/docs/) with a Go core. The desktop app can both host agents and control other devices.

## Current capabilities

- Six-digit device IDs and QR pairing; host confirmation or a host password, saved device grants and revocation.
- Provider architecture, with separate Codex App and Codex CLI integrations.
- Shared session directory, streamed conversation updates, local cache, images and file attachments.
- WebRTC and native TCP direct connections, optional Cloudflare Tunnel, and encrypted WebSocket relay fallback.
- One self-hosted Go server with embedded Web UI and TCP/UDP STUN.

Codex App integration currently targets macOS. Reign must launch the App against its managed shared app-server; an independently launched App is discoverable but cannot be controlled. Remote clients do not require the official Codex App.

## Repository

| Directory | Purpose |
| --- | --- |
| `clients/nsgo` | Maintained desktop, mobile and Web UI |
| `internal` | Go providers, device authorization, transport and storage |
| `cmd/reign-server` | Self-hosted service entry point (`cmd/ras-server` remains compatible) |
| `pkg` | Public service composition, ORM database and connection grant interfaces |
| `deploy/service` | Deployment examples |
| `docs` | Architecture and protocol documentation |
| `clients/flutter` | Preserved earlier prototype; not the maintained UI |
| `deploy/cloudflare` | Preserved Worker prototype; distinct from Cloudflare Tunnel |

## Develop

Requires Go 1.27.3+; native client builds also need the platform SDKs described in [the NSGo client guide](clients/nsgo/README.md).

```sh
go test ./...
cd clients/nsgo
go test ./...
./scripts/build.sh macos_arm64,web
```

## Self-host

```sh
go build -o reign-server ./cmd/reign-server
./reign-server
```

The server listens on **26331/TCP** for Web/API/signaling/relay and **26330/TCP+UDP** for STUN. TLS is optional; a reverse proxy can terminate HTTPS. STUN needs direct TCP/UDP reachability or layer-four forwarding. Release builds embed the compiled Web UI; a plain Go build uses the source placeholder until Web assets are built.

Native settings, device identity and cached conversations use SQLite; browser storage uses IndexedDB. The server defaults to SQLite through GORM and accepts an injected GORM database for other engines. See [storage and Cloudflare setup](docs/storage-and-tunnels.md).

See [server deployment](clients/nsgo/deploy/service/README.md) and [connection architecture](docs/relay.md). Configure the native client default through the build variable `AGENTHUB_SERVER_URL`; Web uses its current origin. No production service is provisioned by cloning this repository.

## Open source and hosted service

The self-hosted edition runs without an official account. A separately maintained private commercial service composes the same connection core, with account ownership, tenant isolation, entitlements and billing in a separate private repository. See [the boundary design](docs/open-source-boundary.md). Business implementation does not belong in this repository.

Old application IDs, protocol identifiers and `AGENTHUB_*` configuration names remain compatibility identifiers during the Reign rename. Historical Flutter notes are in [legacy documentation](docs/legacy-flutter.md).
