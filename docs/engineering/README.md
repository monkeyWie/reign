# Engineering knowledge base

This is the shared reference for contributors and coding agents. [AGENTS.md](../../AGENTS.md) contains global rules and task routing; shared subsystem contracts belong here. Vendor-specific implementation notes belong beside their adapter code. Keep all engineering references in English.

## Find the owning reference

| Change                                             | Reference                             | Main source                                             |
| -------------------------------------------------- | ------------------------------------- | ------------------------------------------------------- |
| Responsibilities, lifecycle, Providers, storage    | [Architecture](architecture.md)       | `internal/app`, `internal/provider`, `internal/control` |
| Go and Worker behavior                             | [Service parity](service-parity.md)   | `internal/deviceservice`, `packages/worker-core`        |
| Appearance, feedback, scrolling, responsive layout | [Frontend design](frontend-design.md) | `clients/nsgo/hubui`, `apps/site`                       |
| Naming, errors, concurrency, platform code         | [Code standards](code-standards.md)   | Both Go modules and the TypeScript workspace            |
| Commands, fixtures, verification limits            | [Validation](validation.md)           | `.github/workflows`, `scripts/ci`                       |
| Connection, authorization, history and UI failures | [Troubleshooting](troubleshooting.md) | `internal/host`, `internal/remote`, `hubui`             |
| Cross-cutting choices and their rationale          | [Decisions](decisions.md)             | Links to the affected contracts                         |

## Focused references

- [Wire protocol](../platform/protocol.md): discovery, pairing, grants, encrypted RPC and progressive history.
- [Deployment](../platform/self-hosting.md) and [Go operations](../../deploy/service/README.md): listeners, persistence, containers and probes.
- [Agent Providers](../platform/agent-providers.md): capabilities, adapters and project metadata.
- [CI and releases](../ci-and-releases.md): build targets, artifacts and publication.
- [Brand icon sources](../../internal/brandicons/README.md): shared assets and licenses.
- [User documentation](../../apps/site/content/docs): multilingual product instructions with consistent content and navigation across supported locales.

## Maintain a reference

Keep each rule in one owning page and link to it. Read the source before treating a document as implementation evidence. Update the reference in the same change when responsibilities, interfaces, state transitions, runtime paths or failure behavior change. Internal refactors that preserve these contracts do not require ceremonial documentation edits.

A subsystem reference should identify responsibilities and boundaries, sources of truth, main runtime paths, important state transitions, failure/recovery behavior and verification entry points. Link files and symbols instead of duplicating changing code. Keep global instructions short; field formulas, Agent ownership rules and individual bug histories belong in focused references.

Standards describe required behavior; they do not certify that every platform already meets it. Mark observations, known gaps and proposals explicitly. Dated `docs/research` reports are evidence for their stated scenario, not automatic product commitments. Do not preserve abandoned prototype entry points or compatibility layers.

Record recurring failure causes and the checks that would catch them in [troubleshooting](troubleshooting.md). Record cross-cutting choices in [decisions](decisions.md). Use temporary identities, example domains and redacted diagnostics; never copy private transcripts or device secrets into the knowledge base.

## Review and handoff

Describe the trigger and observable before/after behavior, affected state and side effects. Trace callers beyond the edited function, look for sibling cases, verify the cited checks and disclose limits. Documentation updates are not proof of correctness. Keep [CONTRIBUTING.md](../../CONTRIBUTING.md) and the public standards navigation connected to this index.
