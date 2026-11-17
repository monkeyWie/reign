# Codex Provider implementation

These notes describe this adapter, not repository-wide standards. The common contract is defined in `internal/provider`; generic architecture is documented in [architecture](../../docs/engineering/architecture.md).

## Runtime paths

- Existing App-owned sessions: discover the local IPC owner and follow its snapshots/patches. Send, append, stop and approval operations go to that owner.
- Reads and new sessions: use the private app-server where required. ReignHub-owned sessions publish follower state to the App. Shutting down this executor can interrupt its tasks; lock release does not guarantee seamless continuation in the App.
- Creation source, current owner and IPC schema version are separate facts. A read or subscription does not force `resume` or acquire a second writer. Owner discovery failures do not authorize lock takeover. An uncertain write is not replayed through a different backend.

See [IPC routing](ipc.go), [owner publishing](ipc_owner.go) and [runtime composition](runtime.go). Dated verification and its limitations are recorded in [the implementation report](../../docs/research/codex-ipc-implementation-2026-11-17.md) and [sync follow-up](../../docs/research/codex-ipc-and-sync-followup-2026-11-17.md).

## State and history diagnosis

Unknown state or patch structures can break watch independently of history. Schema version alone does not identify App versus IDE creation. Read verified v11/v13 state and apply Immer array-path patches transactionally against revisions; reject unknown structures. Initial history and watch run independently, with read-only history fallback and canonical/live merging. The adapter currently publishes v11 owner state; reading v13 is not complete v13 owner support.

Sources and regressions: [state handling](ipc_state.go), [schema](ipc_schema.go), [schema tests](ipc_schema_test.go), [ownership tests](ipc_test.go) and [controller history failure tests](../../clients/nsgo/hubui/session_failure_test.go). Use isolated fixtures; real App behavior and unsupported upstream operations require separate verification.

App-owned turns also contain `steeringUserMessage` items: read their `input`, preserve the local item ID and recorded creation time, and match acknowledged server representations by explicit server/client IDs. Never deduplicate equal text. Merge canonical history with the live item order; a steer can appear before existing Agent output. Item insertion, removal or replacement requires a snapshot because common item events append at the controller.

Asset-bearing live updates invalidate history instead of publishing an unprocessed input envelope. The cancellable transcript reader imports both regular and steering image/file inputs. Keep already displayed cards until their imported replacements arrive. Plain text remains incremental. Regressions cover both verified state versions, acknowledgements, ordering and live image import in [message tests](ipc_messages_test.go).

Windows uses `\\.\pipe\codex-ipc`, installed-package discovery and OS writer-lock checks. The App must be running under the same user, with its local IPC router available. Cross-compilation and isolated protocol tests do not establish real Windows App interoperability.
