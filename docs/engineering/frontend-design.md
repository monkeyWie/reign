# Frontend design standards

These are required interaction and review criteria, not a claim that every platform has already passed acceptance. Known violations are defects.

## Shared system and visual language

Controller UI lives in `clients/nsgo/hubui`; native and Web APIs provide platform access. The official website lives in `apps/site`. Reuse the pinned NSGo components and existing semantic tokens instead of creating another chat UI. Follow the site's directory instructions for Next.js changes.

Use shadcn zinc neutral surfaces with the existing green brand accent. Support light, dark and system appearance. Source controller colors from [appearance.go](../../clients/nsgo/hubui/appearance.go); do not hardcode page-specific palettes.

| Role                             | Light                 | Dark                  |
| -------------------------------- | --------------------- | --------------------- |
| Page / foreground                | `#ffffff` / `#09090b` | `#09090b` / `#fafafa` |
| Muted surface / border           | `#f4f4f5` / `#e4e4e7` | `#27272a` / `#27272a` |
| Secondary text                   | `#71717a`             | `#a1a1aa`             |
| Brand accent / accent foreground | `#16805d` / `#ffffff` | `#7fc6a6` / `#09090b` |

The website keeps its own semantic CSS roles within the same neutral/green identity. Reuse spacing, typography and radius tokens; the current base radius is 10. Use Lucide for actions and [shared Simple Icons assets](../../internal/brandicons/README.md) for recognizable platform, Agent and browser brands. Preserve attribution and licenses. Ordinary offline status is not an error alarm.

Support multiple languages without assuming a fixed language set or pair. Locale configuration defines the supported languages; keep product content, navigation and interactions consistent across them. Layouts must accommodate translated text and locale-aware formatting.

## Every action provides feedback

Every clickable control, including icon buttons, list actions, toolbar controls and menus, must acknowledge activation immediately on the UI loop, before network, disk or Agent work. Hover is not click feedback and is absent on touch devices.

- Provide visible pressed/active and keyboard-focus states through shared components. Give icon-only controls accessible names and tooltips where supported.
- For asynchronous work, enter a local busy state immediately, expose it to assistive technology and show a stable spinner, progress indication or pending label at the action. Block duplicate activation when it would repeat work. Do not block unrelated navigation.
- Scope pending state to the operation and affected resource. Conflicting choices for one request share a gate; another request, session or settings action stays usable. A background completion must not clear another operation's pending state.
- Finish busy state on success, failure or cancellation. Show the result or actionable error. A no-op, unavailable action or offline state needs an explanation, not a silent ignored click.
- Keep form values and the initiating dialog available after failure. Prevent conflicting edits while submitting. Dismissing a submitted write does not cancel or replay it; a late result must not close a newer dialog, change a newer selection or erase a different request.
- Center text-only labels and icon-label groups within their buttons. Loading must not introduce asymmetric empty slots. Keep button bounds and component identity stable while pending; do not restart its animation on unrelated updates. Respect reduced motion. Do not add fake delays or fabricated progress to make feedback visible.
- Manual refresh shows local pending feedback until its request finishes, even when the response contains no changes. Automatic refresh stays visually quiet: it does not animate or disable the manual refresh button or repeatedly announce loading. Preserve existing list content and reading position. A manual refresh supersedes an older background read; stale background results cannot overwrite it.

Use the controller's shared [action controls](../../clients/nsgo/hubui/actions.go) and the website/account [Button](../../packages/account-ui/src/ui.tsx) instead of duplicating pending-state handling. Async button handlers return their operation promise so feedback covers the full request and follow-up refresh. Custom clickable rows also need pressed and focus states; selected appearance does not replace press feedback. Use resource IDs as list keys when actions can remove or reorder rows.

## Dialogs and notification layers

Modal dialogs place an accessible X close control at the top right, reachable in normal, pending, failed and expired states. Keep the footer to at most two decision buttons by default; extra actions need a specific workflow reason. Do not add a third footer button merely to make the dialog dismissible. Informational or expired dialogs may have no footer actions. Use the shared dialog header; custom modal layouts follow the same placement.

Dismissal never implies consent. Expired or withdrawn requests stop offering invalid actions and explain how to start a fresh request; ordinary failures keep a usable retry and close path. Keyboard, back and backdrop behavior must agree with the visible controls. A dismissed request must not immediately reopen without a new user action or request.

Global errors and notifications render above dialog backdrops using the framework's overlay layer, not a larger page-local z-index. Notification actions remain clickable while a dialog is open. The empty area around a notification passes input through without dismissing or blocking the dialog; opening, updating and closing notifications must preserve dialog state, drafts and focus. Check both stacking order and hit testing with a modal present. Keep notifications within the safe viewport on narrow screens.

## Responsive layout

Choose layout by available viewport width, not OS, device name or a guessed mobile flag. [responsive.go](../../clients/nsgo/hubui/responsive.go) owns the current thresholds:

| Available width          | Navigation                                          |
| ------------------------ | --------------------------------------------------- |
| Below 650 points         | Page stack for devices, sessions and conversation   |
| 650 to below 1000 points | Device sidebar plus the active workspace page       |
| 1000 points and above    | Device and session sidebars beside the conversation |

Use shared breakpoints; do not introduce per-page alternatives without updating affected layouts and tests. Check just below, at and above each threshold, resizing and orientation changes. Preserve selected device/session, drafts, focus and reading position when switching layouts. Avoid horizontal page overflow, clipped actions and dialogs wider than the viewport. Content may shrink or wrap; primary actions and back navigation remain reachable.

Card collections such as attachment galleries use container-driven grids: a preferred minimum card width, automatic column repetition and equal flexible tracks, equivalent to `repeat(auto-fill, minmax(minimum, 1fr))`. Column count follows the collection's available width rather than OS or page breakpoints. Choose the minimum and gap for the content using shared spacing; example dimensions are not universal tokens. Clamp the minimum to the container width on very narrow surfaces, set card/text minimum widths to zero and truncate long labels without hiding actions. Preserve source order and stable resource keys as columns change; resizing must not remount cards, restart images or reset pending actions. Use `auto-fill` to retain empty column slots; choose `auto-fit` only when sparse collections should stretch into them. The controller's [cardGrid](../../clients/nsgo/hubui/responsive.go) shares this policy. Verify column transitions, sparse and dense lists, long labels and containers narrower than the preferred minimum. Compact inline chips do not need to become full card grids.

Respect safe areas, text scaling and the soft keyboard. Touch targets need enough hit area even when their icon is small. Check keyboard navigation, contrast, accessible names and non-color status cues. Platform window controls keep their platform style inside the custom title bar. Desktop close hides to tray; explicit quit exits. At the mobile root, back requires the existing second-action confirmation.

Keep the mobile display awake while an active conversation page is visible, including offline cached history. Release the display hold on navigation away, inactivity, backgrounding or disposal; restore it when returning. This is separate from background connection lifetime. Mobile browsers use Screen Wake Lock where supported in a secure context; denial must not block navigation or start an automatic retry loop.

## Scrollbars and scroll behavior

All application scrollbars are hidden at rest, appear only while scrolling and fade after scrolling stops. Use one shared thin overlay style without reserving layout width; do not expose a second native indicator. Apply it to device/session/message lists, settings, dialogs and other scrollable regions. Hover alone does not reveal a persistent bar. Preserve keyboard scrolling and accessible navigation.

[scrollbars.go](../../clients/nsgo/hubui/scrollbars.go) owns controller scrollbar policy. Do not copy its timing or sizing into individual pages. Scroll indicators are distinct from download/progress indicators, which stay visible while their operation is active.

A new touch interrupts inertia immediately in both session and message lists. While following the latest message, sending, content growth and keyboard show/hide preserve the bottom anchor. When the user intentionally reads history, preserve that position and offer the return-to-latest action. Viewport-height changes alone must not be interpreted as user scrolling. On conversation entry, ignore pre-layout scroll events and retain the latest-message anchor through initial history and content measurements. Explicit navigation declares history-reading intent before jumping; layout correction must not undo that jump.

## Connection and message behavior

Device rows show availability, not connection stages or transport paths; failed presence reads are unknown, not confirmed offline. Offline devices remain accessible through cache. Connection status distinguishes connected, connecting and disconnected. Time only the current active attempt. Diagnostics distinguish active transport from prepared standby channels and distinguish network, pairing, authorization and Agent failures.

Send acknowledges immediately and retains a recoverable draft/pending message until acceptance. Explicit failures offer retry next to copy; unknown outcomes display pending confirmation and never offer a replay. Restored in-flight sends are also unconfirmed. Reconcile echoes even after an error response: a write can persist before its acknowledgement or presentation fails. Only a successful complete fresh history read begun after a definitely settled submission may remove an absent failed overlay; partial or paginated history cannot prove absence. Pending and failed overlays retain their original send position using persisted predecessor identities, including when predecessors receive remote IDs or disappear; later replies do not push them to the list end. Long-press copy and selection must survive background updates.

Initial and incremental history render received content progressively in conversation order. History catch-up dots float above the composer without changing list height and disappear when caught up, even if the Agent is still generating. Watch and history loading have separate states. Spinners keep their identity across updates; a typewriter effect must not conceal buffered delivery or delay already received text. Back, cancellation and connection details remain responsive throughout synchronization.

Message navigation runs in conversation order. Clicking a tick jumps directly to the virtualized target without animated scrolling through intervening history. Hover previews open promptly and show the instruction followed by the beginning of its matching Agent response, truncated to a compact size. Do not borrow a response from a later instruction. Keep navigation targets and open previews stable during streaming updates. Nearby ticks may form a short hover wave; honor reduced motion and retain keyboard access.

Tool activity summaries use text emphasis on hover: illuminate the whole label and icon without a full-row background. Preserve the running shimmer's identity, visible press feedback, keyboard focus and expand/collapse behavior.

Attachments expose a visible, accessible download icon on every file/image card. Image activation opens its preview; download activation alone enters the save/download flow, with immediate feedback, actual progress, cancellation and errors. Keep those hit targets independent so download does not also open a preview. Unsupported file previews remain noninteractive content without button semantics or misleading hover feedback; attachment links must not silently start a download. Inline file attachments still expose their download action in the attachment collection. Preserve filenames and extensions. A corner toast alone is not adequate download feedback. User image inputs appear as small, right-aligned previews above their bubble; Agent attachments appear below the reply text. Keep preview and download hit targets separate in both layouts.

Upload cards enter a queued/pending state immediately. Show actual acknowledged progress, capped at 99% until the host confirms completion. Compact cards use percentage, progress and icons rather than status prose; provide localized accessible labels and tooltips. Preserve image identity during progress updates. Queue excess files behind the shared concurrency limit instead of rejecting them as busy.

Connection diagnostics use passive samples from existing authenticated traffic: request response time, transfer rate, recent request failures, pending requests and connection/data age. Request response time includes host processing and is not network RTT; request failures are not packet loss. Omit metrics unavailable for the active implementation; use an unavailable placeholder only for supported metrics awaiting a sample. Do not add probes, traffic or new polling loops solely for this panel.

## Web startup progress

Use one overall bar and percentage, without byte sizes or extra small bars. Within it, display real downloaded ranges at their segment positions so parallel requests are visible. Completed segments join without permanent gaps. Distinguish download, compilation and initialization; downloaded bytes do not mean the application is ready.

## Review and verification

Check light/dark, wide/narrow, touch/keyboard and the changed platform behavior. Exercise delayed, failed, cancelled and unchanged-result actions; verify feedback on the actual control rather than merely checking the request ran. Test breakpoint boundaries, long content, keyboard transitions, scroll anchors and idle/active scrollbar states. Supply relevant screenshots and use the [validation guide](validation.md). Native inertia and system-window behavior still need real-device checks.
