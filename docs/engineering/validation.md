# Validation standards

Select checks by affected behavior. Documentation-only changes do not require native rebuilds; one runtime's unit tests do not prove service parity. Commands below start at the repository root unless stated otherwise.

| Change                        | Relevant checks                                    | Additional evidence                                                 |
| ----------------------------- | -------------------------------------------------- | ------------------------------------------------------------------- |
| Go business/transport/auth    | Affected package tests and vet                     | Race checks for concurrency; Worker review for public contracts     |
| Worker/public service API     | Worker check/test, matching Go tests, common smoke | Both deployments, rejection and recovery                            |
| Controller UI                 | Affected NSGo tests and target build               | Action feedback, breakpoint boundaries, light/dark, touch/keyboard  |
| Packaging/deployment          | Start the actual package and verify assets/probes  | Go archive/container and Worker delivery                            |
| Website/public docs           | Site check and MDX compilation/build               | Navigation across supported locales; screenshots for visual changes |
| Engineering docs/instructions | Formatting, links and diff checks                  | No new behavior tests or app restart                                |

## Go modules and build entry

Root and `clients/nsgo` are separate modules. Run in each affected module:

```sh
GOWORK=off go vet ./...
GOWORK=off go test ./...
```

Use targeted race checks for concurrency changes, such as `GOWORK=off go test -race ./internal/device ./internal/host` at root. UI checks such as `GOWORK=off go test ./hubui` run from `clients/nsgo`. Linux native tests require suitable graphics/display support; CI uses Xvfb. Cross-compilation is not target-platform execution.

CI race and coverage runs allow 10 minutes per NSGo package on Linux, 20 minutes
on Intel macOS, and 5 minutes per core package. The expanded UI package includes
repeated session-switching and layout regressions whose combined race/coverage
execution exceeds ten minutes on Intel macOS runners; that job allows 40 minutes
including dependency compilation and vet.
Keep individual interaction waits bounded; the package budget does not replace
their responsiveness assertions. Inspect the active test and stack before
attributing a timeout to suite size rather than a stalled operation.

Transient UI feedback uses the scoped NSGo clock. Advance that clock explicitly
in tests instead of waiting for wall time, so instrumented rendering cannot
consume the feedback window before its assertion runs. Check replacement and
workspace disposal as well as initial feedback.

Browser interaction tests run from `clients/nsgo` with an available browser:

```sh
NSGO_BROWSER='/path/to/chrome' GOWORK=off go tool nsgo test -target web -run 'TestInitialHistory' ./hubui
```

Build from root with `./scripts/build.sh TARGET`. Default artifacts go to root `dist/`; explicit relative `-output` paths resolve from the caller's directory. See [client build details](../../clients/nsgo/README.md). The Android Go packaging tool belongs to `clients/nsgo/cmd/android-package`; shared helpers live in root `scripts/`.

After changing the locally running Mac application's runtime/UI, rebuild and restart ReignHub while preserving data. Do not restart Codex App or send real Agent messages as a fixture.

## TypeScript and both services

```sh
pnpm check
pnpm test
pnpm build:web
```

For narrower scope, use `pnpm --filter @reignhub/worker-core check` and `pnpm --filter @reignhub/worker-core test`. Worker tests use local resources, not production state.

Start an isolated target with temporary state and an unused port, then run the matching shared smoke command:

```sh
# Worker test instance
pnpm exec tsx scripts/platform/smoke-worker.mts http://127.0.0.1:8787
# Go test instance
pnpm exec tsx scripts/platform/smoke-worker.mts http://127.0.0.1:18788 --go-server
```

Despite its name, the script tests both runtimes with a fake Agent. Apply local D1 migrations before starting Worker; see [development](../../apps/site/content/docs/contributing/development.mdx). Do not overwrite a running debug instance, database or paired identity. CI also checks Go archives and containers; see [ci.yml](../../.github/workflows/ci.yml).

Go defaults to HTTP 26330 and TCP/UDP probes 26331, all configurable. Plain `go build` does not embed freshly built console assets. Use `pnpm build:web` with an external asset directory for source development, or the package builder for embedded releases. [Operations](../../deploy/service/README.md) and `scripts/ci/smoke-server.py` cover startup, assets, SQLite and probes.

## Documentation and website

```sh
pnpm exec prettier --check AGENTS.md CLAUDE.md 'docs/engineering/*.md'
pnpm --filter @reignhub/site check
git diff --check
```

Verify file links and site slugs. Engineering references use English; public MDX/navigation keeps matching supported translations. Site check includes TypeScript and content consistency, not full MDX compilation. Compile or preview changed MDX; `pnpm build:site` builds the official website. See [CI](../ci-and-releases.md) for website smoke and publication.

## Behavior and recovery

UI fixes exercise actual activation before a delayed response, duplicate activation, failure, cancellation and unchanged results. Verify spinner identity across unrelated updates, scrollbar idle/active states and layouts below/at/above shared breakpoints. Add a behavioral regression when needed; do not merely assert that an implementation field changed.

Connection fixes cover configuration failure followed by recovery, cancellation, fresh retry, stale callbacks and established connections surviving setup budgets. Reads cover missing baselines, cursor gaps/repetition, isolated failures and atomic content/cursor persistence. Writes cover explicit rejection versus lost acknowledgement without automatic replay.

Report commands actually run, environment and limits. Real weak networks, lock screen/background behavior, vendor ownership, multiple windows, inertia and system menus require relevant device evidence.
