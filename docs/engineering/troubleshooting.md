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

## Upload stalls or later images fail immediately

Inspect acknowledged offsets, active/queued file counts and completion separately.
The controller limits active uploads to two and queues the rest. Small initial
chunks expose real progress earlier on slow paths. Lost begin/write/finish
acknowledgements reconcile through the same scoped upload ID; they must not
create a second file or replay an Agent message. Keep progress below 100% until
the host confirms integrity. Four-file, lost-ACK and cancellation fixtures live
in [upload tests](../../clients/nsgo/hubui/attachments_native_test.go); these do
not establish cellular-network throughput.

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
