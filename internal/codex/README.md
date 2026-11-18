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

Session directories enrich the private reader's metadata with the actual App
owner's runtime state. `thread/list` on a separate app-server can report
`notLoaded` for a thread actively executing in the App; it is not authoritative
for that external owner. The adapter probes OS writer locks, follows only held
threads through existing read-only IPC and projects activity into the shared
Session summary. No `resume`, conversation-item hydration, writer takeover or App
restart is used. IPC snapshots remain host-local; directory clients receive
metadata, not unselected conversation content.

Directory enrichment has four concurrent reads and a two-second page budget,
sharing the existing 128-follower cap. Unknown ownership, disconnect or failed
state reads produce unknown activity, never idle/completion. Terminal owner state
is preserved when the writer releases its lock, then directory-only followers
are released; explicitly opened conversation subscriptions remain owned by their
normal lifecycle. Active runtime flags and pending requests represent waiting
for input/approval. `TestDirectoryUsesLiveAppOwnerWithoutOpeningConversation`
uses isolated IPC and real temporary OS locks to cover active/completed state
without operating real Agent conversations.

Recent ordering uses fixed conversation boundaries, not `thread.updatedAt`.
IPC turns supply `turnStartedAtMs` and completed `durationMs`; app-server turns
supply `startedAt`/`completedAt` in seconds. Explicit steering-user creation
times also advance recency. Only a successful completed turn contributes its
end time; streaming durations and assistant/tool timestamps never do. For
directory rows without live boundaries, read only the latest turn metadata via
`thread/turns/list` (`limit: 1`, `itemsView: "notLoaded"`). This is a read-only
metadata request, not conversation hydration. It has four concurrent slots and
a two-second page budget, with an at-most-2,048-entry cache keyed by session and
metadata revision/status. Failures retain known timing and retry on later scans.
Older reads cannot regress a known event time. No full transcript is fetched
for sidebar ordering. `recency_test.go` verifies streaming, steering, completion,
metadata edits, caching and failed-read recovery with isolated fixtures.

Unknown state or patch structures can break watch independently of history. Schema version alone does not identify App versus IDE creation. Read verified v11/v13 state and apply Immer array-path patches transactionally against revisions; reject unknown structures. Initial history and watch run independently, with read-only history fallback and canonical/live merging. The adapter currently publishes v11 owner state; reading v13 is not complete v13 owner support.

Sources and regressions: [state handling](ipc_state.go), [schema](ipc_schema.go), [schema tests](ipc_schema_test.go), [ownership tests](ipc_test.go) and [controller history failure tests](../../clients/nsgo/hubui/session_failure_test.go). Use isolated fixtures; real App behavior and unsupported upstream operations require separate verification.

App-owned turns also contain `steeringUserMessage` items: read their `input`, preserve the local item ID and recorded creation time, and match acknowledged server representations by explicit server/client IDs. Never deduplicate equal text. Merge canonical history with the live item order; a steer can appear before existing Agent output. Item insertion, removal or replacement requires a snapshot because common item events append at the controller.

Asset-bearing live updates invalidate history instead of publishing an unprocessed input envelope. The cancellable transcript reader imports both regular and steering image/file inputs. Keep already displayed cards until their imported replacements arrive. Plain text remains incremental. Regressions cover both verified state versions, acknowledgements, ordering and live image import in [message tests](ipc_messages_test.go).

Preserve assistant `phase` in snapshots and item events so the controller can
distinguish commentary from final answers. When one IPC update both finishes a
turn and extends its answer, publish full authoritative items before the terminal
turn event. Terminal item corrections also use full item events, never text
deltas: controllers deliberately ignore streaming deltas after completion.
`TestIPCCompletionPublishesFullAnswerBeforeTerminalStatus` covers both verified
IPC versions, the final text tail, a late correction and history phase parity.

Preserve `durationMs` from canonical IPC entities as well as the live tail in
normalized turns and watch events, including duration-only corrections after
completion. Prefer Codex's recorded duration; when it is unavailable, a valid
app-server `startedAt`/`completedAt` pair (Unix seconds) supplies elapsed time.
Missing or invalid timing stays unknown. Do not derive duration from receipt time,
message timestamps or summed tool durations. Progressive history must retain the
same metadata as a full transcript.

Windows uses `\\.\pipe\codex-ipc` through Microsoft's `go-winio`, with platform
file suffixes separating it from Unix sockets. The client requests data
read/write, read attributes and synchronize rights; generic write additionally
requests server-instance and attribute/EA rights which a pipe DACL can reject.
Permission denial retains its cause across retries and points to Windows user /
privilege alignment, not an absent App. Do not change the App's ACL or elevate it.

Package discovery uses the current user's registered package names and Win32
`GetPackagePathByFullName`, then validates the package manifest and executable.
Process discovery uses Toolhelp and process handles; it launches no PowerShell or
process-listing command. Actual CLI metadata probes and private stdio backends use
the shared `system.HideConsole` Windows adapter (`CREATE_NO_WINDOW` + `HideWindow`).
The App integration attaches automatically when installed, running and enabled;
it never launches or restarts the official App.

Windows fixtures cover restricted named-pipe data exchange and a console-less
child with redirected IO/cancellation. These tests require native Windows to run;
cross-compilation and isolated protocol tests do not establish real Windows App
interoperability or confirm the user's pipe ACL.
