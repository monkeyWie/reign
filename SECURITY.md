# Security policy

Reign is in development preview and has not undergone an independent security audit.

## Reporting a vulnerability

Use the repository's **Security → Advisories → Report a vulnerability** entry if private vulnerability reporting is enabled. Otherwise, open an issue containing only a request for a private reporting channel, without exploit details. A maintainer will arrange a channel before you share the report. Do not include secrets or private session content in public issues.

Include affected versions, platform, prerequisites, impact and a minimal isolated reproduction. Do not test against other users' devices or the hosted service without permission. Use a self-hosted test deployment and disposable identities.

Never submit pairing keys, device access passwords, OAuth tokens, signing material, billing credentials or unredacted chat transcripts. If a credential was exposed, revoke it at its source; deleting the issue is insufficient.
