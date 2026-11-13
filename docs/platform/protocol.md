# Current connection protocol

There is no `/v1` or `/v2` fallback. All connectivity endpoints share `/p2p/`.

| Endpoint | Purpose |
| --- | --- |
| `/p2p/config` | Service identity, lease public key, STUN/TCP discovery and login capability |
| `/p2p/devices/register` | Register a public key and device kind; return a credential and connection code |
| `/p2p/devices/presence` | Update name and coarsened last-seen |
| `/p2p/devices/resolve` | Resolve connection code; online state is authoritative socket state |
| `/p2p/pairings/request` | Submit an encrypted pairing intent |
| `/p2p/pairings/pending` | Host reads pending intents |
| `/p2p/pairings/result` | Controller reads its own result |
| `/p2p/pairings/respond` | Host consents or denies; encrypted reply contains pairing key |
| `/p2p/pairings/revoke` | Host revokes the pairing and closes controller links |
| `/p2p/signal` | WebSocket handshake, signaling and lease renewal only |
| `/p2p/relay` | Authorized write-side ingress for encrypted business frames |

JSON HTTP endpoints use POST (config also accepts GET). Identity tokens are opaque random bearer credentials. Browser WebSocket authentication uses subprotocols `reign.p2p` and `auth.<base64url(token)>`. Tokens are never placed in query strings.

The signal welcome assigns a peer, protocol `1`, `allowRelay` and a short signed grant. When permitted, a sender attaches a relay socket with that signal peer. Business frames arriving on relay are delivered to the recipient's signal socket, keeping a single read stream and peer namespace. The recipient does not need a second read loop for business traffic.

The room injects authenticated controller identity and signed grant into forwarded frames. Caller-supplied identity/grants are ignored. Signal sockets accept only hello, signal and renewal; business frames cannot be moved onto that free ingress. Signal size and rate are bounded. Idle heartbeat replies bypass Durable Object JavaScript; client-driven lease renewal wakes it only periodically.

Pairing uses X25519, HKDF-SHA256 salt `reign/pairing`, AES-256-GCM with per-message random nonce and domain-separated context. A PSK-authenticated hello exchanges fresh nonces; session keys bind PSK, peer and both nonces. Data has directional counter nonces, authenticated context, replay protection and bounded 12,000-byte chunk reassembly. Mutations are never transparently retried after uncertain delivery.

Lease format is `base64url(JSON).base64url(Ed25519 signature)` signed over `reign/grant/v1/<payload>`. Grants bind service namespace, host, controller, role, expiry, product families and connect/relay scopes. Native controlled hosts obtain the verification key from their configured service and verify grants on RPC dispatch **and response**, including direct transports. Missing/expired grants deny remote operations without terminating local Agent work. Nil product list means unrestricted self-hosted use; an empty list means no Agent access.

A paid entitlement enables fallback, rather than forcing relay use. Native clients continuously negotiate WebRTC/UDP and TCP; browsers negotiate WebRTC. Connection loss ends associated peer lifetimes and reconnect begins with fresh handshake randomness. Transcript watch cursors reset to a full snapshot on epoch/replay-window changes; ordinary updates are incremental.
