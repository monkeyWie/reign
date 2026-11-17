# Contributing to ReignHub

Start with the [contribution guide](https://reignhub.app/docs/contributing) and [local development](https://reignhub.app/docs/contributing/development). Their editable MDX source is in `apps/site/content/docs/contributing/`.

Repository instructions are in [AGENTS.md](AGENTS.md). The [engineering knowledge base](docs/engineering/README.md) links the current [architecture](docs/engineering/architecture.md), [frontend design](docs/engineering/frontend-design.md), [code standards](docs/engineering/code-standards.md) and [validation commands](docs/engineering/validation.md). Update the relevant knowledge page when changing a contract or shared behavior.

- Keep each PR focused. Explain the trigger, resulting behavior, validation and known limits.
- Run `gofmt` for Go and Prettier for TypeScript/MDX. Check both Go modules when affected.
- Use isolated Agent fixtures, temporary data and local Worker resources. Never test by changing real user sessions.
- UI changes include desktop and narrow-screen screenshots, covering light and dark themes.
- Public service changes follow the [Go/Worker parity contract](docs/engineering/service-parity.md). Review both implementations, update affected code and types, and describe validation against both. If one implementation needs no edit, explain why. Deployment-specific changes may affect only one runtime.
- Declare only implemented Provider capabilities, and update the Agent support matrix.
- Do not commit binaries, local databases, keys or private transcripts. Follow [SECURITY.md](SECURITY.md) for vulnerabilities.

Web/Worker: `pnpm check`, `pnpm test`, `pnpm build:web`. Go: `GOWORK=off go vet ./...`, `GOWORK=off go test ./...` in each affected module (repository root and `clients/nsgo`). Select checks by scope using the validation guide; documentation-only changes do not require native rebuilds. Native UI tests require a suitable platform/toolchain. Platform build coverage is enforced by GitHub Actions.

By contributing, you confirm you have the right to contribute your changes under this repository's MIT license.
