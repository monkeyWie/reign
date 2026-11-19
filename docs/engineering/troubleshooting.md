# Troubleshooting knowledge

Diagnose discovery, network setup, authorization, RPC, Provider and UI/cache separately. Record build, platform, deployment, trigger, time range, active attempt and path with redacted diagnostics. Fix and verify recovery, not only clean startup.

## Missing authorization public key

**Symptom:** Pairing exists and the service is reachable, but device-response verification fails; controller retry alone does not recover.

**Cause:** Failed host discovery previously fell back to STUN-only configuration and copied an empty key into long-lived connection options.

**Recovery:** `device.HostNetwork` validates service identity and Ed25519 keys. `host.Manager` stays unready and retries until valid. Do not disable authorization or recreate pairing. Keep signing identity across restarts.

**Evidence:** [Configuration](../../internal/device/http.go), [validation tests](../../internal/device/http_test.go), [failure-to-recovery regression](../../internal/host/network_config_test.go).

## Connected device with missing history

A live subscription failure must not gate initial history or erase cached content. Check history, watch and cache independently; distinguish an unavailable Provider from an empty conversation. Missing baselines must not be skipped using only a saved cursor. A missing Provider discovery response must not hide a persisted first metadata page. Only a successful full directory reconciliation can remove stale entries; offline `sync` returns an error and preserves the directory. Long history transfers use per-request progress idle budgets, not an additional fixed total UI timeout. Vendor schema and backend investigations belong with the adapter implementation notes.

**Evidence:** [History failure tests](../../clients/nsgo/hubui/session_failure_test.go), [controller stream tests](../../clients/nsgo/hubui/session_stream_test.go).

If switching away and back restores stale local Codex messages, inspect the
follower as well as the saved replay cursor. The cursor can remain valid after
the upstream subscription disappears, or a directory-only follower can be
released between turns. Watch/probe must restore a missing follower and retain
an explicitly opened one through directory cleanup. All controllers retain
synchronization while their window, activity or tab is hidden and execution is
still allowed. Distinguish OS/browser freezing from an application cancelling its
own work; check visibility transitions as well as upstream ownership before
treating stale content as a transport failure.
See [Codex follower recovery](../../internal/codex/ipc_follow_test.go).

If the directory spins but the conversation has no running controls or progress
until completion, compare the transcript's turn status with the current owner.
The controller previously retained any cached terminal turn over an incoming
running snapshot and ignored explicit running turn events. This permanently
rejected subsequent live items/text until a final snapshot arrived. Accept the
current snapshot and ordered lifecycle transition; retain the terminal guard for
unsolicited late item/text deltas. The stream must replay newer completion events
over a delayed history response before merging it.

`TestRunningOwnerReplacesTerminalCacheBeforeCompletion` reproduces both snapshot
and event recovery through visible UI; `TestDelayedRunningSnapshotReplaysCompletionBeforeMerging`
checks that old history and late deltas do not resurrect a finished reply. These
isolated regressions establish the failure mode, not the provenance of a real
cached terminal status. Read-only live diagnostics after reopening confirmed
incremental delivery; the original stalled state was not captured end to end.

## Desktop remains on an old running reply after Codex finishes

Window occlusion is a background lifecycle transition on macOS. Previously the
controller applied the mobile suspend policy to desktops too, cancelling watch,
reads and cache progress while the host Provider kept running. The UI could keep
partial commentary and an old running state even after a fresh history read had
already returned the final answer; resuming only a valid replay cursor was not
an independent freshness check.

In a 2026-11-19 read-only diagnosis, the controller's saved UI snapshot retained
an in-progress turn while its latest RPC history and a fresh IPC owner snapshot
contained the completed answer. Bringing ReignHub forward resumed synchronization.
The shared policy now retains subscriptions on desktop, mobile and Web while
hidden and coalesces one history refresh on foreground return, preserving the
watch, editor and draft. Android foreground-service denial or loss does not
cancel healthy subscriptions. This fixes the reproduced lifecycle path; it is
not evidence that every upstream synchronization failure has the same cause.

[Lifecycle regressions](../../clients/nsgo/hubui/lifecycle_sync_test.go) deliver
completion while hidden, verify final text/status and persisted cursor for
local/remote routes, retain directory/approval reads, and recover a silent
completion on foreground return without restarting the watch or replaying
commands. [Connection recovery](../../clients/nsgo/hubui/connections_test.go)
also completes while backgrounded; [Android service tests](../../clients/nsgo/hubui/background_test.go)
cover absent/denied/lost protection. Application cancellation still ends work.
These isolated fixtures inject lifecycle transitions; they do not establish
physical-device background execution under iOS/Android power management or
browser tab freezing. No user conversation is operated as a test.

A later lock/unlock report also showed an already submitted message in the
pending queue. The scoped read-only cache inspection happened after recovery:
both UI and RPC caches then contained the final answer and an empty queue. That
does not capture the original stalled state or prove which notification was
missed. Isolated fixtures did reproduce another gap: a populated, silent IPC
follower could indefinitely satisfy both a valid watch cursor and a supposedly
fresh transcript read using old memory.

Repair uses a fresh-snapshot barrier and queue delivery receipts; old cached state
alone cannot establish a completed repair. Failed reads retain the persisted
history fallback without acquiring a writer or replaying a send. See
[Codex state recovery](../../internal/codex/README.md#state-and-history-diagnosis).

A subsequent report recovered automatically without session switching. Its
screenshot was taken about six minutes after recorded completion, but the first
cache inspection was after recovery. That did not capture the original failure.
A temporary 30-second history/full-state check covered missed updates but did not
explain their cause; it has been removed from the normal synchronization path.

Further inspection found two concrete protocol mismatches. First, the installed
Codex producer resets stream revisions on backend reconnect while retaining its
IPC client. ReignHub discarded the resulting lower-revision snapshots and all
subsequent lower patches as old, leaving a connected stream visibly frozen.
Second, a new owner's following-status announcement was filtered against the old
owner, preventing prompt subscription handoff. Full snapshots now establish a new
revision base, and owner announcements invalidate the subscription and wake its
watch for verified rediscovery immediately. Neither repair waits for a periodic
history scan. An announcement is only a hint, never permission to route writes.

[Stream lifecycle tests](../../internal/codex/ipc_lifecycle_test.go) failed before
the fix and now recover through notifications alone. A read-only
[installed producer check](../research/verification/codex-stream-2026-11-19/validate.cjs)
executes the packaged producer on synthetic state and confirms a reset from
revision 500 to snapshot 1, followed by patch 2. This establishes an actual
vendor-contract mismatch, not the provenance of the original live screenshot.
[Controller tests](../../clients/nsgo/hubui/session_reconcile_test.go) cover
notification-driven recovery, ordinary heartbeats without history downloads,
local/remote and hidden views, failed subscriptions, backoff, concurrent live
updates, preserved drafts and cancelled navigation. First open, foreground
return and detected subscription errors still reconcile; healthy steady-state
subscriptions do not poll full history.

## Completed answer buried under progress or missing its final text

Inspect message phase and event order separately. Rendering every assistant item
as a permanent reply leaves progress commentary above the answer. Use explicit
phase metadata for collapsed process details, retaining unknown messages and
commentary when no answer is available. Completion can also share a batch with
the final text tail: a terminal event sent first makes later text deltas stale.
Publish authoritative final items before the terminal event and accept terminal
item corrections, including shorter corrected text. Never infer successful
completion from disconnect or silently discard the only available response.

**Evidence:** [Controller completion tests](../../clients/nsgo/hubui/work_summary_test.go)
and [Codex IPC completion regression](../../internal/codex/ipc_messages_test.go).

## Repeated Agent errors on a connected device

An installed Agent can be unavailable while other Agents on the device are
healthy. Reading every history-capable Provider repeatedly requests unavailable
Agents even though the network works. Check readiness before directory reads;
preserve cached sessions and rediscover readiness on the next refresh. Never
start unrelated Agent processes to repair another Provider's synchronization.
Real failures from ready Providers remain errors and identify their Provider ID.

**Evidence:** [Catalogue recovery regression](../../clients/nsgo/hubui/catalogue_test.go).

## Windows console flashes or App access denied

Repeated console flashes can come from periodic executable probes. Use native
Win32 discovery and compile-time platform adapters. `HideWindow` alone can still
allocate a console; background Agent commands also need `CREATE_NO_WINDOW` while
retaining their pipes and cancellation. Do not hide the symptom by slowing polls.

Named-pipe `Access is denied` does not mean the App is closed. The default
`go-winio` dial requests generic read/write; generic write can include unnecessary
server rights. Request only client communication rights, preserve the real error
through retries, and check user / privilege alignment if denial persists.

Matching the desktop Apps' privilege levels is not sufficient if a different
environment owns the pipe. In a reported Windows case, an SSH → VS Code → Node
process chain held `codex-ipc` with write access restricted to administrators and
SYSTEM, while both desktop Apps ran as ordinary users. Closing that environment's
Codex instance let desktop Codex App take over the pipe and restored access.
On access denial only, ReignHub now queries the server PID through a metadata-only
handle and traces a bounded Toolhelp snapshot. Recognized SSH / VS Code ancestry
adds a possible-source hint and asks the user to close the corresponding instance
in that environment, reopen desktop Codex App and retry. `node.exe` alone is not
source evidence. Ancestry can be stale or unavailable; diagnosis does not prove
the ACL's cause, and unknown/failed queries retain generic permission guidance.

Never terminate user processes, broaden the vendor pipe ACL or automatically run
either App as administrator. Tests change only unique fixture pipe ACLs and verify
denial, metadata PID lookup and successful data exchange after removing denial.
Native Windows execution and real App integration remain separate verification
steps; a cross-build does not establish either.
See [Windows adapter notes](../../internal/codex/README.md),
[console fixture](../../internal/system/console_windows_test.go) and
[restricted-pipe fixture](../../internal/codexipc/local_windows_test.go).

## Relay, WebRTC and TCP logs disagree with the panel

Relay preparation, temporary relay use, WebRTC selection and later TCP standby readiness can all occur during parallel setup. The last channel-established event is not necessarily the active business path. Distinguish standby readiness from path selection.

When signaling loss also ends business traffic, inspect context ownership, explicit Close, renewal and peer cleanup. `use of closed network connection` alone does not identify an ISP/Cloudflare failure. Setup timeouts must not own established connection lifetimes; budgets live in [retry.go](../../internal/connectivity/retry.go).

## Slow relay sync or only user messages appearing

Separate network delay, large-response write occupancy, history reads, Provider failures and UI-loop blocking. Multiple logical requests still share transport bandwidth; enqueueing one large JSON response can delay smaller replies.

Check progressive history, frame scheduling and budgets against [the protocol](../platform/protocol.md). Do not compensate with unbounded timeouts. Directory/history/watch must progress independently; isolate failed sessions and render received content in order. Inspect actual requests/cursors rather than assuming incremental sync never needs a baseline or history page.

**Evidence:** [Provider stream tests](../../internal/provider/stream_test.go), [RPC](../../internal/rpc/client.go), [controller stream tests](../../clients/nsgo/hubui/session_stream_test.go), [read retry](../../internal/readretry). Measure first visible content, cancellation and small-request latency; fixtures do not establish real weak-network performance.

## Cancelled history continues on the host

A request can reach the host before its local frame writer reports completion. If caller cancellation wins that completion race, an early send-error return must still cancel the scoped remote read. Both native and browser clients send a bounded cancellation independently of the cancelled caller, without disconnecting the connection or cancelling Agent writes.

**Evidence:** [Deterministic write/cancellation regression](../../clients/nsgo/browserclient/cancellation_native_test.go), [native read scheduling](../../internal/remote/read_scheduling_test.go). The regression holds write completion after receipt; increasing a test timeout would hide the defect.

## Stale connecting timers or unresponsive retry

Check active-attempt state, start/end time, generation and latest events together. Historical running events must not keep a timer alive. A retry creates a fresh attempt and cancels obsolete setup; resumed foreground state rejects stale callbacks. Display diagnostic timestamps with timezone context.

**Evidence:** [Attempt tests](../../clients/nsgo/hubui/connect_attempt_test.go), [diagnostic log](../../clients/nsgo/hubui/connection_log.go), [recovery tests](../../internal/clienthub/reconnect_test.go).

## Missing action feedback, reset spinners or jumping lists

A handler starting network work does not itself acknowledge activation. Check shared active styles and local busy state, accessible labels, duplicate suppression and completion/failure cleanup. A refresh returning unchanged content must still show that its request ran. Test the visible control while the response is held.

Spinner resets often indicate component replacement, not incorrect animation duration. Check node identity and avoid rebuilding whole histories on text deltas. Catch-up dots must not consume list height and must disappear once caught up even while generation continues. Preserve bottom anchors across keyboard/viewport changes and selection/focus across background updates. See [frontend design](frontend-design.md).

A keyed virtual row can still remount if its `Dynamic` constructor reads the
whole row value reactively after checking an identity memo. Capture construction
data untracked; keep changing labels, indentation and tooltip contents in their
own reactive properties. Test real directory updates, not only unrelated notice
updates, and check both node identity and animation progress while the hint is
open. See [sidebar stability tests](../../clients/nsgo/hubui/sidebar_stability_test.go).

The same trap applies above the list: a workspace constructor that reads the
environment or palette can subscribe its entire subtree to appearance changes. Keep layout
and conversation boundaries dependent on their breakpoint/selection memos, and
read child construction data untracked. Verify spinner and editor identity, not
just restored draft text, across theme changes on the browser/native targets.
See [activity and appearance regression](../../clients/nsgo/hubui/session_activity_test.go).

If simultaneous loading rings rotate at different angles, their animations have
independent start times. Bind them to the workspace's shared rotation value in
[loading.go](../../clients/nsgo/hubui/loading.go). Its owner must outlive individual
rows; mounting a late row joins the current phase, and removing the last ring
stops the animation. [The synchronization regression](../../clients/nsgo/hubui/loading_test.go)
covers staggered starts, disposal and reduced-motion changes.

## Mac exits while switching remote conversations

A remote device's platform does not identify the failing process. Capture the
controller's stderr and inspect the stack before changing transport or Provider
retries. A reported Mac crash while switching Windows conversations was a nil
view in NSGo's macOS `surface.apply`, handling `ItemStickyCells`.

Retained scroll content can update after leaving the mounted tree. The previous
NSGo pin could emit sticky/scroll commands for an unplaced view, and the native
backend dereferenced the absent view. The client pins NSGo `v0.2.0`, which includes
upstream fix [`7774099b6513`](https://github.com/nsgo-dev/nsgo/commit/7774099b6513a61f33199764420d3372859cdfe6)
to drop unplaced commands and guard missing native views. Keep the fix in
the pinned dependency; do not patch the module cache or hide the panic with a
global recovery handler.

[Session switching regressions](../../clients/nsgo/hubui/session_switching_test.go)
use synthetic Windows sessions and detached scroll content. The detached-content
case reproduces the old pin's missing-view failure in the test driver; the rapid
switching case also runs in a native Mac window. Native tests alone do not prove
the original remote-device scenario has recovered. Never enable full render
traces against real conversations: they include rendered text. Capture only the
error stack needed for diagnosis.

## Recent sessions repeatedly swapping order

Do not sort by a general `updatedAt`: output chunks and metadata can change it,
and a directory refresh can alternate with an older selected-session snapshot.
Use the fixed `recencyAt` conversation boundary instead (submission or successful
reply completion), preserving the latest known value across directory, history
and live events. Status still updates independently. No clock value should be
created by a poll, cache restore, navigation or text delta.

**Evidence:** [Recent ordering regression](../../clients/nsgo/hubui/session_recency_test.go)
and [Provider boundary/caching tests](../../internal/codex/recency_test.go).

## Upload stalls or later images fail immediately

Inspect acknowledged offsets, active/queued file counts and completion separately.
The controller limits active uploads to two and queues the rest. Small initial
chunks expose real progress earlier on slow paths. Lost begin/write/finish
acknowledgements reconcile through the same scoped upload ID; they must not
create a second file or replay an Agent message. Keep progress below 100% until
the host confirms integrity. Four-file, lost-ACK and cancellation fixtures live
in [upload tests](../../clients/nsgo/hubui/attachments_native_test.go); these do
not establish cellular-network throughput.

## File links have no download information

The controller can download a remote Markdown file link only when its message
contains a matching exported attachment. A missing record does not prove the
file is missing. Older hosts do not export all Windows paths or source-location
links; update ReignHub on the executing device as well as the controller, then
reopen the conversation to refresh its transcript. Files outside the session
workspace and Hub attachment directory remain unexported by design. A matched
attachment marked unavailable is a different case: check the file on its device.
Never resolve a remote path against the controller's filesystem or bypass the
scoped download API. See [Codex file links](../../internal/codex/README.md#file-links)
and [routing tests](../../clients/nsgo/hubui/message_links_test.go).

Informational feedback must use the shared notice helper, rather than directly
assigning display text. Otherwise a missing-download hint or success message can
remain indefinitely. [Notice regressions](../../clients/nsgo/hubui/notices_test.go)
cover expiry, replacement, disposal, narrow layouts and dismissal above a modal.

## Add a lesson

Record trigger, evidence, cause, recovery, regression entry and unverified conditions. Mark hypotheses explicitly. Link implementation/tests rather than copy them. Dated investigations belong in `docs/research`; reusable lessons belong here. Do not grow global instructions with individual bug rules.

## Accepted messages followed by an App error page

A failed RPC acknowledgement is not proof that a message was rejected. For the
Codex adapter, inspect the App error-boundary exception and stack separately
from the steering rejection. Missing `text_elements` in optimistic text input
causes a renderer `length` dereference; messages can remain persisted and become
visible after the App reloads canonical history. Normalize at the vendor input
boundary, verify using installed-package code and synthetic state, and preserve
unknown delivery outcomes through RPC/bridge errors. Never reproduce by sending
into a real conversation or restarting Codex App as a fixture. Vendor details and
verification commands belong in `internal/codex/README.md`.

For controller entry/keyboard issues, test initial delayed history and content
remeasurement as well as manual virtual-list jumps: a bottom-anchor fix must not
undo explicit history navigation. Failed local overlays need stable predecessor
positions across streaming, cache restore and remote echo replacement. A partial
history page cannot authorize deletion of an absent message.

## Submitted text reappears in the composer

The delivery row and input field are separate state. A delayed native change
notification carrying the exact submitted value must not resurrect the consumed
draft. Track the submitted value and route only until a fresh editing action;
keyboard, paste and composition actions permit entering the same text again.
Consume Return's final editor value and never clear a newer draft when the
network acknowledgement arrives. Do not remount the editor to force a reset.

**Evidence:** [Composer submission tests](../../clients/nsgo/hubui/composer_submit_test.go)
cover late echoes, identical new drafts, repeated native Return and offline
submission. The original intermittent report has not been captured with a native
event trace; these fixtures verify the guarded failure mode, not its prevalence.
