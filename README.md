# ReignHub

Remote control for the AI Agents on your own computers. Desktop, mobile and browser controllers share one NSGo + Go UI. The website and documentation use Next.js and Fumadocs. The default service is `https://reignhub.app`.

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/monkeyWie/reignhub)

## Website and documentation

The official site is [reignhub.app](https://reignhub.app), with [quick start](https://reignhub.app/docs/quick-start), [self-hosting](https://reignhub.app/docs/self-hosting), [technology](https://reignhub.app/technology) and [contribution guides](CONTRIBUTING.md). Documentation uses Fumadocs on Next.js; edit Markdown/MDX in `apps/site/content/docs` and each folder’s `meta.json` for sidebar order. Run `pnpm dev` for a content preview. The complete Worker build also includes the static browser-search index.

## Architecture

- **One Worker** hosts `/`, `/docs`, `/chat` and `/p2p/*`. D1 stores device identities and encrypted pairing envelopes. Hibernating Durable Objects route signaling and encrypted relay frames.
- **Direct first:** native clients try WebRTC/UDP and native TCP in parallel. Browsers use WebRTC. Public and optional self-hosted discovery servers provide candidates concurrently. If direct connectivity fails, self-hosted clients use their own Worker relay.
- **One service address:** clients discover optional probe endpoints from `/p2p/config`. No second service address is required in the UI.
- **End-to-end encryption:** pairing uses X25519/HKDF/AES-GCM; session traffic uses fresh directional AEAD keys and replay protection. Servers do not hold chat pairing keys or store chat plaintext.
- **Providers:** Codex App and CLI are separate controlled connections, grouped into one `codex` product family for optional policy. Codex App must be connected through ReignHub's shared app-server setup. Local Agent tasks continue independently of remote connection authorization.
- **Caching:** native clients use SQLite. Browsers use IndexedDB for paired identities, snapshots and bounded attachment caching.

The open-source deployment has no account, subscription, or commercial device limits. Our hosted service composes this same public core with private identity, payment and entitlement policies. Cloudflare's own infrastructure limits and charges still apply to your deployment.

## Development

Requires Go (see `go.mod`), Node 24 and pnpm 10.34.5.

```sh
pnpm install
pnpm check
pnpm test
pnpm build:web
pnpm exec wrangler d1 migrations apply DB --local
pnpm exec wrangler dev --port 8787
```

Open `http://localhost:8787/chat`. Browser cryptography and WebRTC require a secure context: HTTPS or localhost. Use the generated `workers.dev` HTTPS address for other computers and phones.

All controller UI development lives in [`clients/nsgo`](clients/nsgo). NSGo maps this shared UI to native views on desktop/mobile and Go WebAssembly + DOM in browsers. `/chat` serves a standalone HTML shell and compressed, content-addressed NSGo assets.

## Self-hosting

The button above deploys the **whole repository**, not an isolated monorepo subdirectory. Cloudflare provisions the D1 database and SQLite Durable Object. The deploy command applies the D1 schema after provisioning. No OAuth applications, payment configuration, domain or external VM are required. Set every client to the resulting service origin.

For manual deployment, run `pnpm build:web`, `pnpm exec wrangler login`, then `pnpm run deploy`. See [deployment and optional probes](docs/platform/self-hosting.md).

An optional Go probe improves native TCP/UDP candidate discovery. It runs independently:

```sh
go build -o reignhub-probe ./cmd/reignhub-probe
./reignhub-probe --listen :26330
```

Expose **26330/TCP and 26330/UDP** directly. The probe contains no website, device directory, account database, or relay. No TURN, DERP, cloudflared or Tailcat is required.

## Source map

| Directory              | Responsibility                                            |
| ---------------------- | --------------------------------------------------------- |
| `apps/site`            | Public website, docs and NSGo console hosting           |
| `packages/account-ui`  | Website account and administration pages     |
| `packages/client-sdk`  | Pairing, encrypted RPC, WebRTC and IndexedDB              |
| `packages/protocol`    | Current `/p2p/*` protocol and policy interfaces           |
| `packages/worker-core` | D1 directory/pairing, signed leases and hibernating rooms |
| `clients/nsgo`         | Shared desktop, Android, iOS and browser UI                        |
| `internal`             | Native Go application, Providers, transport and SQLite    |
| `cmd/reignhub-probe`      | Optional TCP/UDP discovery binary                         |
| `.github/workflows`    | Native, Web, Worker and probe checks/builds/releases      |

Protocol compatibility with older prototypes is intentionally not provided. See [implementation scope](docs/platform/implementation.md) and [protocol](docs/platform/protocol.md). Older documents outside `docs/platform` describe historical prototypes.
