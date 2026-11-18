# Codex optimistic text input verification

The local App logs for 2026-11-18 contained repeated `LocalConversationPage`
error-boundary failures: `Cannot read properties of undefined (reading 'length')`.
The stack resolves to the installed renderer's text-input parser, which reads
`e.text_elements.length` without a default. ReignHub's simplified text inputs
omitted this array. No conversation content or identifiers are included here.

`validate.cjs` reads `/Applications/ChatGPT.app/Contents/Resources/app.asar` as
bytes and extracts that parser from the renderer bundle. An isolated VM proves
that the old simplified shape throws, then verifies adapter-normalized text and
image input. The parser SHA-256 at verification was
`8f82c56328d76e2efd1dad8394f831cb86968d8e2e1794a037b4fb1dc8d81658`.

Run from the repository root:

```sh
REIGNHUB_CODEX_ASAR=/Applications/ChatGPT.app/Contents/Resources/app.asar \
  GOWORK=off go test ./internal/codex -run 'TestInstalledApp(TextInput|Steer)Contract' -v
```

The check fails when upstream extraction boundaries change. It does not connect
to the running App, inspect private session state, submit prompts, execute a
model or restart the App. It verifies the identified failing parser and the
synthetic steer entry, not the entire vendor UI or every future input schema.
Protocol fixtures separately cover completed-turn routing, published input,
uncertain outcomes and absence of replay/private-writer fallback.
