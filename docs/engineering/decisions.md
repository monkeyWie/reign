# Architecture decisions

These are accepted maintenance choices, with details and verification in the owning references. Acceptance is not certification of every real platform.

| Decision                                           | Rationale and consequences                                                                                                          | Reference                                                                  |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| One public contract, Go and Worker runtimes        | Replaceable deployment without client forks; both need semantic and interoperability checks                                         | [Service parity](service-parity.md)                                        |
| One shared NSGo controller                         | Shared behavior/state with platform capability adapters                                                                             | [Architecture](architecture.md), [frontend design](frontend-design.md)     |
| Console-only self-hosted packages                  | Website source stays public, with separate official publication and smaller service packages                                        | [Deployment](../platform/self-hosting.md), [CI](../ci-and-releases.md)     |
| Common Provider model, controller grouping         | Hosts resolve project and execution directories; controllers group across Agents and devices using host-resolved project identities | [Providers](../platform/agent-providers.md)                                |
| Bounded progressive recoverable sync               | Ordered early content, independent history/watch, atomic content/cursor persistence, isolated failures                              | [Protocol](../platform/protocol.md), [troubleshooting](troubleshooting.md) |
| Strict retryable trust configuration               | Missing public keys cannot masquerade as readiness; recovery preserves pairing                                                      | [Troubleshooting](troubleshooting.md)                                      |
| Retry idempotent reads, reconcile uncertain writes | Lost acknowledgements must not duplicate messages, approvals or sessions                                                            | [Code standards](code-standards.md)                                        |
| No abandoned internal-testing compatibility layers | Protocol/cache redesign is allowed; identity protection and recovery remain required                                                | [Repository instructions](../../AGENTS.md)                                 |

## Unified local and remote control (2026-11-18, accepted)

Device-first navigation prevented a global view of Agent work and made local
management follow a different controller path. Use one Caller contract and one
session directory for local and remote environments. Embedded execution bypasses
transport/authentication setup while sharing dispatch, receipts and cache/sync
behavior; remote control adds the authenticated network layer. The sidebar owns
flat Pinned/Active sections and a configurable All sessions tree. Active means
running or awaiting input, rather than a recently visited inbox; All stays ordered
by creation time. Unknown native creation timestamps remain unknown.

The cost is per-environment directory state and fully scoped UI identities. Project
merging requires host-provided repository identity; equal folder names are unsafe.
The Go and Worker public service contract is unchanged because both forward opaque
RPC payloads. Verification belongs to the environment, cache-race, shared local
execution and responsive UI fixtures; it does not certify live vendor credentials
or physical-device weak-network behavior.

## Record a change

For a cross-cutting decision, record the problem, choice, costs, affected boundaries and evidence. Ordinary local fixes do not need a long decision record.

```text
Date/status: proposed, accepted, superseded or retired
Problem: trigger, user impact and evidence
Decision: chosen behavior and boundaries
Alternatives: rejected choices and costs
Impact: controller, Go, Worker, Providers, storage, deployment
Verification: results and unresolved conditions
Supersedes: previous decision/reference, if applicable
```

Public interface decisions also follow service parity. Performance/competitor claims require measured versions and workloads; a possibility found in research is not an implemented capability.
