# Codex Provider implementation

These notes describe this adapter, not repository-wide standards. The common contract is defined in `internal/provider`; generic architecture is documented in [architecture](../../docs/engineering/architecture.md).

## Runtime paths

- Existing App-owned sessions: discover the local IPC owner and follow its snapshots/patches. Send, append, stop and approval operations go to that owner.
- Reads and new sessions: use the private app-server where required. ReignHub-owned sessions publish follower state to the App. Shutting down this executor can interrupt its tasks; lock release does not guarantee seamless continuation in the App.
- Creation source, current owner and IPC schema version are separate facts. A read or subscription does not force `resume` or acquire a second writer. Owner discovery failures do not authorize lock takeover. An uncertain write is not replayed through a different backend.

See [IPC routing](ipc.go), [owner publishing](ipc_owner.go) and [runtime composition](runtime.go). Dated verification and its limitations are recorded in [the implementation report](../../docs/research/codex-ipc-implementation-2026-11-17.md) and [sync follow-up](../../docs/research/codex-ipc-and-sync-followup-2026-11-17.md).

## App follower writes

The App follower steer entry requires a composer `restoreMessage` even when a
`clientUserMessageId` is supplied. Read the current owner's state and send the
same message ID with the prompt, cwd and composer context (workspace roots,
collaboration mode and attachment collections). Missing restore state causes
its JavaScript executor to read `cwd` from undefined before issuing `turn/steer`.
Do not fall back to a private writer or replay the failed write automatically.

`TestIPCSteerPreservesComposerRestoreContext` verifies the transmitted payload.
The opt-in `TestInstalledAppSteerContract` reads the installed ASAR only and
executes its steer entry with synthetic state and an intercepted follower route:

```sh
REIGNHUB_CODEX_ASAR=/Applications/ChatGPT.app/Contents/Resources/app.asar GOWORK=off go test ./internal/codex -run TestInstalledAppSteerContract -v
```

This reproduces the missing-context error and validates the supplied composer
shape without connecting to a real App conversation or running a model. It is
not evidence of a completed real App send. Reinspect upstream code when this
check reports a changed entry or boundary.

## Optimistic input and delivery outcomes

App IPC input is stricter than app-server input. Every text input requires a
`text_elements` array before the App renders its optimistic user message. The
adapter normalizes text at both send/steer boundaries and in owner publications,
including text references accompanying files; existing rich elements survive.
Missing arrays can crash the App conversation page even after a message has
persisted. Server acceptance does not imply successful App rendering.

Steer routing checks the current owner's latest turn before dispatch: an ended
turn uses a new-turn request. A race rejection is returned without automatically
replaying the write. Only verified pre-submission rejections count as definite
failure; arbitrary IPC JavaScript errors are uncertain. Preserve that distinction
through the encrypted RPC and native JSON bridge for controller reconciliation.

`TestIPCTextInputsSafeForOptimisticAppRendering` covers transmitted send/steer
payloads. `TestInstalledAppTextInputContract` reads the installed ASAR and executes
the actual failing renderer parser with synthetic input, reproducing the missing
array failure before validating the normalized input. Run with the same
`REIGNHUB_CODEX_ASAR` environment variable as the steer contract check. No live
App writes, model execution or restart is involved. See the [verification
notes](../../docs/research/verification/codex-input-2026-11-18/README.md).

## State and history diagnosis

Unknown state or patch structures can break watch independently of history. Schema version alone does not identify App versus IDE creation. Read verified v11/v13 state and apply Immer array-path patches transactionally against revisions; reject unknown structures. Initial history and watch run independently, with read-only history fallback and canonical/live merging. The adapter currently publishes v11 owner state; reading v13 is not complete v13 owner support.

Sources and regressions: [state handling](ipc_state.go), [schema](ipc_schema.go), [schema tests](ipc_schema_test.go), [ownership tests](ipc_test.go) and [controller history failure tests](../../clients/nsgo/hubui/session_failure_test.go). Use isolated fixtures; real App behavior and unsupported upstream operations require separate verification.

App-owned turns also contain `steeringUserMessage` items: read their `input`, preserve the local item ID and recorded creation time, and match acknowledged server representations by explicit server/client IDs. Never deduplicate equal text. Merge canonical history with the live item order; a steer can appear before existing Agent output. Item insertion, removal or replacement requires a snapshot because common item events append at the controller.

Asset-bearing live updates invalidate history instead of publishing an unprocessed input envelope. The cancellable transcript reader imports both regular and steering image/file inputs. Keep already displayed cards until their imported replacements arrive. Plain text remains incremental. Regressions cover both verified state versions, acknowledgements, ordering and live image import in [message tests](ipc_messages_test.go).

Windows uses `\\.\pipe\codex-ipc`, installed-package discovery and OS writer-lock checks. The App must be running under the same user, with its local IPC router available. Cross-compilation and isolated protocol tests do not establish real Windows App interoperability.
