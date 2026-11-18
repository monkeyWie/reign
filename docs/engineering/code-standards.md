# Code standards

## Scope and review

Read the owning reference and implementation before editing. Follow the whole path from user input through callers, state and side effects. Fix sibling occurrences of the same defect, and recheck consumers when changing a predicate, breakpoint, ordering rule or async step. Avoid unrelated refactors and speculative frameworks.

Keep rules in their owning reference; comments explain reasons and constraints. Engineering documents and instructions use English. Public product copy retains its supported translations. Do not add compatibility layers for abandoned internal-testing protocols or caches; preserve identities, keys and user choices.

## Go

- Use `gofmt`, concise packages and business-oriented names. Interfaces express the caller's needed capability rather than expose the full application.
- Pass cancellation through `context.Context`; identify its owner and cleanup. Request setup contexts must not become the permanent lifetime of established connections.
- Wrap identifiable causes with `%w`; prefer typed errors and `errors.Is` / `errors.As`. Centralize unavoidable vendor text classification at the adapter boundary.
- Every goroutine, timer and subscription has an owner and termination path. Bound concurrency, queues, buffers, reassembly and deduplication. Report limits without silently discarding content and advancing its cursor.
- Never block on network, process, channel or disk work under a lock needed by its consumer. Publish shared state before handing it off; shallow copying a map does not isolate it. Check generation before applying async results.
- Every OS-dependent capability uses compile-time-selected files and narrow adapters, including tests and explicit unsupported fallbacks. Use recognized suffixes (`*_windows.go`, `*_darwin.go`) or `//go:build` constraints for platform groups; do not branch on `runtime.GOOS` in shared capability implementations. Use pinned dependencies and public NSGo APIs; do not modify module caches or introduce a runtime for one platform convenience.
- On Windows, inspect and control processes with Win32 calls through `golang.org/x/sys/windows` / syscall. Do not spawn PowerShell, `cmd`, `tasklist` or similar utilities for discovery. Actual Agent executables may use `os/exec` (which invokes CreateProcess); configure their platform attributes in Windows-only code. Background probes and stdio executors use `CREATE_NO_WINDOW` as well as `HideWindow`, preserving redirected IO and cancellation. Named pipes use Microsoft's `go-winio` with the minimum client access rights; never widen another application's ACL or elevate it automatically.

## TypeScript and UI

Use workspace checks and Prettier. Validate external input from `unknown`; do not use `any` or assertions to bypass protocol/authentication checks. Handle cancellation and errors, and release browser resources with their owning component/connection.

Follow installed Next.js documentation for the website. Keep server secrets out of client bundles. Use the shared controller UI and [frontend design rules](frontend-design.md), including immediate action feedback, shared responsive breakpoints and scrolling-only indicators.

## Data and failure semantics

A failed read or parse is unknown, not an empty list, equality or permission to overwrite existing data. Preserve last-known usable state, report failures and fail closed for security decisions. Do not let defaults overwrite explicit user configuration.

Define fields, units, nullability, status codes and failure behavior together. Go/TypeScript types are manually maintained; follow [service parity](service-parity.md), not an assumed generator. Take vendor field types and semantics from verified upstream evidence, not names alone.

Separate definite failure from uncertain execution. Centralize read/setup retry policies; commands, approvals and creation must not inherit transparent read retries. Validate authorization, scope and idempotency at trusted boundaries, not only with disabled UI controls.

Persist content and its applied cursor atomically. Separate device, Provider and session ownership. Services retain only required identities, permissions and opaque pairing data, not conversation plaintext. Bound and redact diagnostics; never log credentials, signed grants, keys, attachment contents or private transcripts.

## Verification and handoff

Test observable behavior, boundaries and failure-to-recovery transitions using temporary state and fake Agents. Do not operate real conversations as fixtures. Add meaningful regressions for behavioral fixes; do not add implementation-mirroring tests for documentation or minor reversible styling.

Run [scope-appropriate checks](validation.md), including race checks when lifecycle/concurrency changes. Report builds, fixtures and real vendor integration separately. Performance claims need version, platform, build, workload and measurement method. Finish checks before claiming success.

Describe the concrete trigger, final behavior, evidence and limits. Update affected contracts and reusable failure knowledge in the same change. Publication requires explicit authorization.
