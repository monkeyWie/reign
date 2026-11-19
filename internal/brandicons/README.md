# Shared brand icons

`paths.json` contains monochrome SVG paths on a 24 × 24 viewBox, embedded locally
by the NSGo client and available to the website. Preserve the upstream paths and
render with semantic foreground colors; never fetch artwork during navigation.
Artwork is from [Simple Icons](https://github.com/simple-icons/simple-icons),
licensed under [CC0 1.0](https://github.com/simple-icons/simple-icons/blob/16.38.0/LICENSE.md).
Brand names and logos remain the property of their respective owners.

| Keys                                                                                                       | Pinned Simple Icons release                                                           | Use                                                                                                                                                   |
| ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apple`, `windows`, `android`, `linux`, `chrome`, `edge`, `firefox`, `safari`, `opera`, `brave`, `vivaldi` | [11.15.0](https://github.com/simple-icons/simple-icons/tree/11.15.0/icons)            | Existing platform/browser artwork; upstream slugs include `windows11`, `googlechrome`, `microsoftedge`, `firefoxbrowser`.                             |
| `openai`                                                                                                   | [11.15.0](https://github.com/simple-icons/simple-icons/blob/11.15.0/icons/openai.svg) | The unified Codex integration uses its OpenAI brand, independent of session creation source. OpenAI is absent from 16.38.0.                           |
| `claude`, `cursor`, `opencode`, `deepseek`, `googlegemini`, `pi`                                           | [16.38.0](https://github.com/simple-icons/simple-icons/tree/16.38.0/icons)            | Claude Code, Cursor, OpenCode, DeepSeek Harness, Gemini CLI and Pi. The `pi` asset's upstream source is `https://pi.dev/favicon.svg`, not Pi Network. |

The controller mapping lives in
[`brand_icons.go`](../../clients/nsgo/hubui/brand_icons.go), shared by the home
overview, Agent settings, session rows, Agent tree nodes and conversation identity.
Select logos by exact Provider ID, never by arbitrary display-name substrings or
by the selected model. Grok Build, Antigravity, Kiro, Muse Code and fx have no
matching entry in the checked 16.38.0 catalog; they and unknown/custom Providers
use a neutral Lucide Bot fallback. Do not assign an unrelated brand to fill a gap.
