# Self-hosted ReignHub

## Standalone Go deployment

Go and Cloudflare Worker are independent deployment options with the same `/p2p/*` contract. The Go service does not need Cloudflare, an account service, Node at runtime or an external database. Both deployments include only the Web console and redirect `/` to `/chat/`; the official website/docs are built and published separately.

```sh
docker compose -f deploy/service/compose.yaml up -d --build
# Or extract a standalone release and run:
./reignhub-server
```

All features are enabled by default: device registration, pairing, signaling, encrypted relay, Web, UDP STUN and TCP STUN. HTTP defaults to `:26330`; TCP/UDP discovery defaults to `:26331`. Native relay uses `:26332` on UDP and TCP. SQLite defaults to `data/reignhub.db`. Edit the bundled `config.json` and restart to change listeners or services. For a binary-only installation, create the template once with `--init`. Preserve the database and signing identity on upgrades.

For a Worker probe only, set `"http": false` in the Go configuration. This mode opens no HTTP listener and no SQLite database. TCP and UDP ports can differ. Release packages include verified Homebrew/Scoop definitions; their tap/bucket publication is a separate maintainer step. See [Go deployment, HTTPS, systemd and package managers](../../deploy/service/README.md).

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
go build -o reignhub-server ./cmd/reignhub-server
# Set config.json to {"http":false} before starting.
./reignhub-server
```

Open `26331/TCP` and `26331/UDP` on the VM and firewall. Both protocols use the same port number. These are STUN Binding/probe listeners, **not HTTP endpoints**, so an Nginx HTTP reverse proxy cannot carry them. A Cloudflare DNS record for this probe must be **DNS only**, not proxied.

Set Worker variables:

```text
EXTERNAL_STUN=["probe.example.com:26331"]
```

`PUBLIC_STUN` defaults to Cloudflare and Google UDP STUN. Public and self-hosted candidates are gathered concurrently; it is not a waterfall that waits for public relay failure. TCP probes supply native TCP mappings and simultaneous-open candidates. A browser has no raw TCP socket API and only attempts WebRTC.

No probe means LAN/IPv6 and public UDP STUN still work. Native public TCP discovery benefits from this optional probe. Some NATs still cannot be traversed; neither TCP nor UDP hole punching guarantees a direct route. Self-hosted Worker WSS is the fallback.

## Add external native relays and STUN servers

Both open-source control services can advertise static nodes. On each external Go machine, set the control service's HTTPS origin in the node's `config.json` and start the node. Run the second command in another shell from the same directory.

```json
{"http": false, "relayControl": "https://control.example", "state": "data/relay.db"}
```

```sh
./reignhub-server
./reignhub-server --print-relay-pin
```

Keep the node's state file on a persistent volume. Allow inbound TCP and UDP on 26331 for STUN and 26332 for native relay. The relay uses a self-generated TLS identity and makes outbound HTTPS requests to the control service for admission and renewal. Clients need direct access to the node's IP or DNS-only hostname. A STUN-only node can use `{"http":false}` and requires no pin.

**Go control service.** Merge the following fields into its `config.json`. Substitute the node's real address and printed pin. Restart the service with `./reignhub-server`. The extra STUN lists extend the local discovery endpoints. To assign every native session to external nodes, set `"nativeRelayListen": ""` on the control service.

```json
{
  "externalStun": ["203.0.113.10:26331"],
  "externalRelays": [
    { "addresses": ["203.0.113.10:26332"], "pin": "REPLACE_WITH_PRINTED_PIN" }
  ]
}
```

**Cloudflare Worker control service.** Merge these entries into the active Wrangler configuration's `vars` and redeploy. `EXTERNAL_STUN` accepts a list of shared UDP/TCP addresses. Both Go and Worker use address arrays. Prefix an entry with `udp:` or `tcp:` to advertise only that transport, e.g. `["udp:probe.example.com:3478","tcp:probe.example.com:3479"]`. The same syntax applies to each relay’s `addresses`. A relay has one PIN and at most one distinct public endpoint per transport. Use bracketed IPv6 such as `tcp:[2001:db8::1]:26332`. Both external node variables accept Wrangler JSON values or their JSON text in the dashboard. Existing deployments can continue using `SELF_STUN` (UDP) and `TCP_STUN` when `EXTERNAL_STUN` is absent. An explicit empty list clears both external STUN lists.

```json
{
  "vars": {
    "EXTERNAL_STUN": ["203.0.113.10:26331"],
    "EXTERNAL_RELAYS": [
      { "addresses": ["203.0.113.10:26332"], "pin": "REPLACE_WITH_PRINTED_PIN" }
    ]
  }
}
```

For example, a Go relay can listen on port 26332 internally and expose UDP 26332 and TCP 443 via NAT. Register `{"addresses":["udp:203.0.113.10:26332","tcp:203.0.113.10:443"],"pin":"REPLACE_WITH_PRINTED_PIN"}` on either control. Protocol-specific relay addresses require native clients that support split endpoints. Shared addresses use the original signal welcome format.

Each control service supports up to 32 static external relays. With multiple entries, the host ID determines one node for both legs. Go includes its enabled local listener as one additional choice. A native connection can fall back to the authorized WebSocket path when the selected node is unreachable. Browsers use WebRTC and WebSocket. Check STUN entries with `GET /p2p/config` and look for `Relay · QUIC` or `Relay · TCP` on a native connection using the relay. Each node serves one configured control origin.

## Persistence and privacy

D1 stores device registration credentials as hashes, public keys, names, pairing relations, encrypted pending envelopes and a persistent service signing key. Pending pairing records are removed after expiry. The server does not store chat plaintext or attachment content. Paired control devices hold the keys needed to read messages; revoke a lost device at the controlled computer.

Browsers cache locally with IndexedDB (attachments limited to approximately 100 MiB). Native caches use SQLite and local attachment files. Clearing browser data requires pairing again; it does not by itself revoke the old device credential on the host.
