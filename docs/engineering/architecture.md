# Architecture standards

## Responsibilities and sources of truth

The controller owns interaction and local cache. The controlled host runs Agent adapters and execution. The device service provides discovery, pairing, signaling and encrypted relay; it does not run Agents or store conversation plaintext.

```mermaid
flowchart LR
  UI[Shared NSGo UI] --> Client[Controller API and cache]
  Client --> Local[In-process Caller]
  Local --> Dispatcher
  Client --> Transport[End-to-end encrypted RPC]
  Transport --> Dispatcher[Host dispatcher]
  Dispatcher --> Provider[Common Provider model]
  Provider --> Adapters[Vendor adapters and execution backends]
  Client -. Discovery and pairing .-> Service[Go or Worker service]
  Transport -. Encrypted relay fallback .-> Service
```

| Layer                  | Source                                                           | Owns                                                              |
| ---------------------- | ---------------------------------------------------------------- | ----------------------------------------------------------------- |
| Presentation           | `clients/nsgo/hubui`                                             | Layout, interaction, view state, controller-side grouping         |
| Controller application | `internal/clienthub`, `clients/nsgo/webapi`                      | Paired identity, connections, cache and RPC                       |
| Host application       | `internal/host`, `internal/app`                                  | Configuration, authorization, lifecycle, Provider composition     |
| Business model         | `internal/control`, `internal/provider`                          | Dispatch, sessions, messages, approvals, capabilities             |
| Agent adapters         | `internal/codex`, `internal/agents`                              | Vendor protocols, processes, local session access                 |
| Transport              | `internal/remote`, `internal/transport`, `internal/connectivity` | Encryption, active paths, frame scheduling, probes, retry budgets |
| Device service         | `internal/deviceservice`, `packages/worker-core`                 | The common public service contract                                |
| Persistence            | `internal/store` and cache adapters                              | Transactions, permissions, ownership and bounded caches           |

Presentation does not own Agent processes, database connections or vendor schema interpretation. Providers do not import UI or branch on the service runtime. Services route ciphertext without interpreting tool output. Compose dependencies at the application boundary.

## Shared UI and alternative services

Desktop, mobile and browsers share the NSGo controller; native and Web adapters express capability differences without copying pages or state machines. Next.js/Fumadocs serves the separately built official website.

Go uses SQLite, HTTP/WebSocket and optional integrated TCP/UDP probes. Worker uses D1, Durable Objects and platform bindings. Align external behavior, not table layouts or implementation lines; see [service parity](service-parity.md). Both self-hosting options remain complete and account-free. Their packages include the console and service; marketing and docs are published separately.

## Lifecycle and state transitions

Separate page, connection-attempt, established-connection, read-request and Agent-execution lifetimes. A remote timeout or phone disconnect must not implicitly stop a local Agent task. Explicit interruption and shutdown of a ReignHub-owned executor are distinct events.

Agent integrations default enabled. The host persists explicit choices in
`Config.AgentEnabled`; missing keys mean enabled, and unavailable integrations
remain visible for installation/setup guidance. Each `AutoConnector` has its own
cancellable setup budget and retry loop, independent of the device service and
other Agents. Automatic setup never sends a prompt, changes credentials or
restarts an external application. Codex App attaches when installed and running;
CLI attachment still requires its compatible shared backend. ACP/Pi presets may
initialize their owned protocol process when installed and configured.

The local-only `host.agent.enabled` action changes this preference. The dispatcher
decorates the same Provider status for local and remote callers and rejects new
calls while disabled. Disabling cancels pending automatic setup, but does not
terminate external Apps or already accepted Agent tasks; existing caches remain
readable. Re-enabling resumes automatic attachment. Configuration and disablement
are not remote pairing permissions, and remote callers cannot change them.

Bound connection setup and individual requests; established connections and subscriptions need independent cancellation, liveness and cleanup. Bound queues, event logs, reassembly and concurrency. Cancel obsolete reads, and check context, device/session identity and generation before publishing results. UI state belongs to the UI loop; I/O and parsing run outside it.

Initial history, live watch and approvals progress independently. Publish history progressively in order. Provider discovery is availability metadata, not directory deletion authority. Persist first directory pages immediately and load them independently of current Provider discovery; replace a Provider directory only after successful complete reconciliation. Offline or failed reconciliation never commits an empty directory.

Directory refresh reads only Providers currently marked ready. A history capability
does not authorize requests to an unavailable Provider. Preserve its cached
sessions and recheck availability on subsequent discovery; synchronization resumes
when the Provider becomes ready. Scope actual directory errors to the Provider.

Persist received content with its applied cursor; a cursor must never acknowledge missing content. Rebuild only the affected baseline after replay-window loss. Isolate session failures with their own backoff.

## Provider boundary and session metadata

Normalize vendor data into `Session`, `Turn`, `Item`, `Approval` and implemented `Capabilities` at the adapter boundary. Do not forward raw notifications, private reasoning or tool output as conversation text.

The host supplies `projectDir`, optional opaque `projectKey`, actual execution directory `cwd`, and creation/update timestamps in Unix milliseconds. Separate `recencyAt` records
the latest user submission or successful reply completion; metadata and streaming
updates must not advance it. Missing timing stays unknown. The same boundary
metadata travels in session summaries, turns and turn events. Controllers group matching repository project keys across devices, falling back to device plus `projectDir || cwd`. Names and vendor project IDs are not directory identity. A controller must not inspect its own filesystem to infer a remote project. Explicit project roots and worktree resolution follow the [Provider reference](../platform/agent-providers.md).

Document vendor-specific discovery, ownership transfer, local protocols and backend selection beside the relevant Provider code.

## Local and remote environments

`control.Caller` is the controller execution boundary. Native composition attaches the
application-owned `app.Embedded` runtime to `clienthub.Hub` as `local`; remote
connections implement the same interface through authenticated encrypted RPC.
Both routes share `Dispatcher.HandleRequest`, Provider dispatch, mutation ordering,
durable execution receipts, directory reconciliation and controller caches.
`HandleRemote` rejects local administration before entering the shared dispatcher.
Closing a controller or disconnecting a peer never closes the local runtime.
The local environment is ready independently of service registration, pairing,
account policy and remote network status. Mobile and browser controllers have no
local executable environment.

Each known environment has its own cached directory, cancellation scope and refresh
generation in `hubui/catalogue.go`. The landing sidebar starts all directories
without choosing a device. Saved valid grants resume once on entry; an initial
failed resume keeps cache available and offers an explicit retry. Established
connections continue the existing recovery/backoff policy. Manual disconnect
persists disabled auto-resume. Refresh and navigation never replay writes.

A provider's first page can appear before its complete reconciliation. Slow disk
cache fills only missing entries for providers without a completed live scan, so
unavailable Agents remain visible without resurrecting deleted sessions. Late
callbacks are fenced by environment instance and generation. Navigation changes
only the selected execution route and its transcript subscription, not directory
lifetimes. Session identity, drafts, pins and list keys include environment,
Provider and session IDs; deduplication never crosses environments. Pin, grouping
and tree expansion preferences are controller-local.

## Authorization and recovery

A host becomes remotely ready only after obtaining a valid service identity and authorization public key. Failed discovery must remain retryable; public-STUN defaults cannot substitute for missing trust configuration. Preserve service identity and signing keys across restarts.

Local pairing permission, signed service leases and optional commercial policy are separate boundaries. Validate authorization on direct and relay paths. Distinguish network failure, expired authorization and unavailable Agent capabilities; account-free deployments must not suggest signing in to fix ordinary network errors.

Record cross-cutting changes in [decisions](decisions.md), including lifecycle, persistence, runtime and verification effects. Internal testing permits direct protocol/cache redesign; identity preservation and failure recovery remain required.
