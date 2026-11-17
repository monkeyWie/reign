# Codex App Computer Use availability investigation

## Report and scope

The supplied screenshot shows the Computer use settings page with only a
Google Chrome row and the subtitle **Disabled by your organization or unavailable
in your region**. This is an availability decision in settings, not a session
writer-lock error. Inspecting an App-owned session's execution configuration is
not necessary to explain this particular UI state.

Inspection used the installed `/Applications/ChatGPT.app` package, reporting
version `26.1111.21308` on 2026-11-17. Only local package source and selected
configuration keys were read. No App files, configuration, account state or
real conversations were modified, and Codex App was not restarted.

## Verified implementation

- `webview/assets/async/28918.48c9cffd9d.js`: the Computer use settings component
  obtains external browser availability through module `jWaO`. A local host
  whose check has finished and whose `allowed` result is false gets the
  restricted Chrome row. Its `sa` description component chooses the screenshot's
  text when `isRestricted` is true. Missing extension installation has a
  different description.
- `webview/assets/async/907538.0572f09d40.js`, module `jWaO`: external browser
  availability combines the host's `browser_use_external` feature, a remote
  feature gate, account/configuration requirements and platform restrictions.
  Several failures produce the same settings description.
- The same asset's `GGpr` module requires a successful requirements result and
  checks `requirements.allowBrowserAndComputerUse`. An absent requirements
  result is not treated as permission. Module `IIEq` independently checks the
  broader Computer Use capability, including its own feature gate and host
  configuration. A Chrome availability failure does not identify which of
  these broader checks also failed.
- ReignHub's current [Provider](../../internal/codex/provider.go) ConnectDesktop
  path starts its own integration; it does not call the legacy system launcher,
  restart the official App or set its WebSocket environment override. The
  current adapter does not write Computer Use feature/requirement settings.

## Conclusion and limits

The screenshot establishes that the official App has classified Chrome control
as unavailable. It does not establish that the account is organization-managed,
that the region is the cause, or that IPC broke a session. The inspected path
contains no IPC owner lookup. No evidence was found that ReignHub changed the
official App's availability settings. Do not enable a deprecated MCP entry or
override feature/organization gates as a workaround.

To distinguish the remaining causes, inspect the official App's supported
account/workspace availability and feature status, or provide diagnostics to
OpenAI support if a previously entitled account has lost access. A private
ReignHub-owned executor's tool configuration is a separate integration question;
this screenshot neither proves nor tests that capability. Browser automation
runtime discovery logs alone do not prove full Computer Use availability.
