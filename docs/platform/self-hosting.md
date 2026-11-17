# Self-hosted ReignHub

## Standalone Go deployment

Go and Cloudflare Worker are independent deployment options with the same `/p2p/*` contract. The Go service does not need Cloudflare, an account service, Node at runtime or an external database. Both deployments include only the Web console and redirect `/` to `/chat/`; the official website/docs are built and published separately.

```sh
docker compose -f deploy/service/compose.yaml up -d --build
# Or extract a standalone release and run:
./reignhub-server --config server.json
```

All features are enabled by default: device registration, pairing, signaling, encrypted relay, Web, UDP STUN and TCP STUN. HTTP defaults to `:26330`; TCP/UDP discovery defaults to `:26331`. SQLite defaults to `data/reignhub.db`. Customize every listener and feature through JSON or CLI; explicit flags override the file. Preserve the database and signing identity on upgrades.

For a Worker probe only: `reignhub-server --http=false`. This mode opens no HTTP listener and no SQLite database. TCP and UDP ports can differ. Release packages include verified Homebrew/Scoop definitions; their tap/bucket publication is a separate maintainer step. See [Go deployment, HTTPS, systemd and package managers](../../deploy/service/README.md).

Browser crypto requires HTTPS or localhost; LAN HTTP origins are suitable for native testing, not arbitrary remote browsers. STUN sockets require direct reachability and cannot be carried by an HTTP reverse proxy.

## One-click Cloudflare deployment

Use the root repository Deploy to Cloudflare button. The template needs the root pnpm workspace; Cloudflare's deploy button does not include sibling dependencies when given a monorepo subdirectory.

The template provides one Worker, static NSGo WASM console assets, one D1 database and one SQLite Durable Object class. D1 is automatically provisioned by Wrangler/Workers Builds; `pnpm run deploy` deploys the Worker, then applies the schema. Initial requests may briefly return unavailable before that first migration finishes. No paid-plan enforcement exists in this deployment. Cloudflare's own free-tier resource allowances still apply.

After deployment, paste the resulting `https://<worker>.<account>.workers.dev` origin into clients. The browser controller lives at `/chat`. The website and docs are excluded. No custom domain is required.

The one-click resource provisioning requires a real Cloudflare account and has not been exercised by local dry-run tests. The compiled Worker and D1 migrations are tested locally.

## Manual deploy

```sh
corepack enable
pnpm install --frozen-lockfile
pnpm build:web
pnpm exec wrangler login
pnpm run deploy
```

Wrangler provisions missing draft resources. Keep the assigned database ID in your deployment configuration for later releases. Back up D1 before future schema changes. D1 metadata includes the service signing identity; replacing it invalidates existing short leases until clients refresh their service configuration.

To restrict external Web origins, set `ALLOWED_ORIGINS` to a comma-separated list of exact origins. Requests from this Worker's own origin are permitted automatically. A malicious page cannot bypass device pairing by sending a token in the URL: tokens use Authorization or the WebSocket subprotocol.

For a local Worker, apply migrations with `--local` before `wrangler dev`. Do not point development clients at production state directories.

## Optional discovery probe

```sh
go build -o reignhub-probe ./cmd/reignhub-probe
./reignhub-probe --listen :26331
```

Open `26331/TCP` and `26331/UDP` on the VM and firewall. Both protocols use the same port number. These are STUN Binding/probe listeners, **not HTTP endpoints**, so an Nginx HTTP reverse proxy cannot carry them. A Cloudflare DNS record for this probe must be **DNS only**, not proxied.

Set Worker variables:

```text
SELF_STUN=stun:probe.example.com:26331
TCP_STUN=probe.example.com:26331
```

`PUBLIC_STUN` defaults to Cloudflare and Google UDP STUN. Public and self-hosted candidates are gathered concurrently; it is not a waterfall that waits for public relay failure. TCP probes supply native TCP mappings and simultaneous-open candidates. A browser has no raw TCP socket API and only attempts WebRTC.

No probe means LAN/IPv6 and public UDP STUN still work. Native public TCP discovery benefits from this optional probe. Some NATs still cannot be traversed; neither TCP nor UDP hole punching guarantees a direct route. Self-hosted Worker WSS is the fallback.

## Persistence and privacy

D1 stores device registration credentials as hashes, public keys, names, pairing relations, encrypted pending envelopes and a persistent service signing key. Pending pairing records are removed after expiry. The server does not store chat plaintext or attachment content. Paired control devices hold the keys needed to read messages; revoke a lost device at the controlled computer.

Browsers cache locally with IndexedDB (attachments limited to approximately 100 MiB). Native caches use SQLite and local attachment files. Clearing browser data requires pairing again; it does not by itself revoke the old device credential on the host.
