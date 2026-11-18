# Current connection protocol

There is no `/v1` or `/v2` fallback. All connectivity endpoints share `/p2p/`.

| Endpoint                | Purpose                                                                        |
| ----------------------- | ------------------------------------------------------------------------------ |
| `/p2p/config`           | Service identity, lease public key, STUN/TCP discovery and login capability    |
| `/p2p/devices/register` | Register a public key and device kind; return a credential and connection code |
| `/p2p/devices/presence` | Update name and coarsened last-seen                                            |
| `/p2p/devices/resolve`  | Resolve connection code; online state is authoritative socket state            |
| `/p2p/pairings/request` | Submit an encrypted pairing intent                                             |
| `/p2p/pairings/pending` | Host reads pending intents                                                     |
| `/p2p/pairings/result`  | Controller reads its own result                                                |
| `/p2p/pairings/respond` | Host consents or denies; encrypted reply contains pairing key                  |
| `/p2p/pairings/revoke`  | Host revokes the pairing and closes controller links                           |
| `/p2p/signal`           | WebSocket handshake, signaling and lease renewal only                          |
| `/p2p/relay`            | Authorized write-side ingress for encrypted business frames                    |

JSON HTTP endpoints use POST (config also accepts GET). Identity tokens are opaque random bearer credentials. Browser WebSocket authentication uses subprotocols `reign.p2p` and `auth.<base64url(token)>`. Tokens are never placed in query strings.

The signal welcome assigns a peer, protocol `1`, `allowRelay` and a short signed grant. When permitted, a sender attaches a relay socket with that signal peer. Business frames arriving on relay are delivered to the recipient's signal socket, keeping a single read stream and peer namespace. The recipient does not need a second read loop for business traffic.

The room injects authenticated controller identity and signed grant into forwarded frames. Caller-supplied identity/grants are ignored. Signal sockets accept only hello, signal and renewal; business frames cannot be moved onto that free ingress. Signal size and rate are bounded. Idle heartbeat replies bypass Durable Object JavaScript; client-driven lease renewal wakes it only periodically.

Pairing uses X25519, HKDF-SHA256 salt `reign/pairing`, AES-256-GCM with per-message random nonce and domain-separated context. A PSK-authenticated hello exchanges fresh nonces; session keys bind PSK, peer and both nonces. Data has directional counter nonces, authenticated context, replay protection and bounded 4,096-byte chunk reassembly. Mutations are never transparently retried after uncertain delivery.

Lease format is `base64url(JSON).base64url(Ed25519 signature)` signed over `reign/grant/v1/<payload>`. Grants bind service namespace, host, controller, role, expiry, product families and connect/relay scopes. Native controlled hosts obtain the verification key from their configured service and verify grants on RPC dispatch **and response**, including direct transports. Missing/expired grants deny remote operations without terminating local Agent work. Nil product list means unrestricted self-hosted use; an empty list means no Agent access.

A paid entitlement enables fallback, rather than forcing relay use. Native clients continuously negotiate WebRTC/UDP and TCP; browsers negotiate WebRTC. Connection loss ends associated peer lifetimes and reconnect begins with fresh handshake randomness. Transcript watch cursors reset to a full snapshot on epoch/replay-window changes; ordinary updates are incremental.

## Encrypted RPC version 3

The encrypted hello contains `rpc: 3`. Both endpoints require this version;
there is no fallback to older framing. The service welcome protocol remains 1
because the service forwards opaque encrypted frames and does not parse RPCs.

Each encrypted chunk is `{r, id, i, n, e, s, b}`: RPC request ID, transfer ID,
zero-based index, fragment count, `identity` or `gzip`, uncompressed byte count,
and base64 payload. Requests and responses both require the RPC ID. Payloads
are limited to 16 MiB before and after decompression, with at most 4,096 fragments
of 4,096 bytes each. Payloads of at least 1 KiB use gzip at the fastest setting
only when it saves at least 12.5%. Compression happens before encryption.

Per-connection bounded frame queues prioritize live/control traffic, then
session metadata, then transcript and attachment data. Each transfer queues
one frame at a time; one bulk frame is serviced after at most eight other
frames. Per-RPC bounded receive inboxes preserve response order without letting
one slow transcript consumer block the shared socket reader.

A read is cancelled after 30 seconds without its own response-fragment progress.
Other requests and socket heartbeats do not extend that budget. Progressing
transfers have no fixed total timeout. Explicit setup deadlines are absolute.
`request.cancel` names the original read in `cancel`; it never cancels a write.
Cancelled responses' assemblies are discarded immediately. Saturated host
request slots return `HOST_BUSY`; they never silently drop requests. Commands
are never automatically replayed after an uncertain result. A host response with
`code: "WRITE_OUTCOME_UNKNOWN"` means execution may already have completed, even
when its `error` describes an Agent/UI exception. Controllers preserve this code
through local JSON bridges, reconcile the resulting history and do not expose a
write retry while the outcome is unknown. Go and Worker services forward this
encrypted response unchanged and require no service API changes.

Directory reconciliation transfers session metadata deltas. Opening a Codex
conversation loads its latest 30 turns; steady state uses watch events. Plain
user messages do not trigger a history snapshot. Android monitors only the state
of known running tasks through `turn.status`, never their transcript contents.
History is reread for initial loading, replay-epoch gaps, explicit older pages,
and attachment import events that require an authoritative snapshot.

Progressive history follows source turn/item order. A long item's text is sent
in UTF-8-safe parts of at most 2,048 bytes; `textOffset` is the original UTF-8
byte offset. Receivers append verified contiguous/overlapping text, publish
immutable prefixes and remove `textOffset` from assembled snapshots. They
never wait for a whole long item to render its first prefix. Compression is
also applied to these smaller responses when useful. Codex tool activities
retain their typed metadata without forwarding raw stdout/native tool JSON
that the controller does not render.

Initial/reset catch-up journals watch events until the ordered history is
complete, so small later user messages cannot overtake missing assistant
replies. Once a baseline exists, live watch events remain independent of
background snapshot refreshes. Assistant commentary is retained in completed
history, in its source order; completion does not hide prior visible replies.

## Resumable attachment uploads

The controller generates a random 64-character hexadecimal upload ID before
`attachment.begin` and reuses that ID with the immutable filename, size and
SHA-256. Begin returns the existing acknowledged offset, or the completed
attachment. Reusing an ID with a different scope or metadata is rejected.
Writes at an already stored offset verify matching bytes rather than append
duplicates; finish returns the same confirmed attachment when repeated.
Only these idempotent operations may retry a lost acknowledgement. This does
not authorize replaying Agent messages or other writes.

Controllers queue uploads across drafts with at most two active transfers.
Each chunk starts at 4 KiB and adapts up to 48 KiB to observed acknowledgements.
Each attempt is bounded to 15 seconds, with at most three attempts per operation;
removal cancels work and makes a bounded best-effort abort. Progress measures
acknowledged bytes, stays below 100% until confirmation and does not bypass
content integrity validation. Host finish hashes/copies once before publication.

Services continue forwarding opaque encrypted frames: these RPC/cache changes
require no Go device-service or Worker public API changes. Native and browser
controllers implement the same upload and observation semantics in shared UI.
