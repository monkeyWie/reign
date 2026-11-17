# Architecture decisions

These are accepted maintenance choices, with details and verification in the owning references. Acceptance is not certification of every real platform.

| Decision                                           | Rationale and consequences                                                                             | Reference                                                                  |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| One public contract, Go and Worker runtimes        | Replaceable deployment without client forks; both need semantic and interoperability checks            | [Service parity](service-parity.md)                                        |
| One shared NSGo controller                         | Shared behavior/state with platform capability adapters                                                | [Architecture](architecture.md), [frontend design](frontend-design.md)     |
| Console-only self-hosted packages                  | Website source stays public, with separate official publication and smaller service packages           | [Deployment](../platform/self-hosting.md), [CI](../ci-and-releases.md)     |
| Common Provider model, controller grouping         | Hosts resolve project and execution directories; controllers group across Agents within a device       | [Providers](../platform/agent-providers.md)                                |
| Bounded progressive recoverable sync               | Ordered early content, independent history/watch, atomic content/cursor persistence, isolated failures | [Protocol](../platform/protocol.md), [troubleshooting](troubleshooting.md) |
| Strict retryable trust configuration               | Missing public keys cannot masquerade as readiness; recovery preserves pairing                         | [Troubleshooting](troubleshooting.md)                                      |
| Retry idempotent reads, reconcile uncertain writes | Lost acknowledgements must not duplicate messages, approvals or sessions                               | [Code standards](code-standards.md)                                        |
| No abandoned internal-testing compatibility layers | Protocol/cache redesign is allowed; identity protection and recovery remain required                   | [Repository instructions](../../AGENTS.md)                                 |

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
