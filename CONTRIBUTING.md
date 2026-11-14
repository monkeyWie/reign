# Contributing to ReignHub

Start with the [contribution guide](https://reignhub.app/docs/contributing) and [local development](https://reignhub.app/docs/contributing/development). Their editable MDX source is in `apps/site/content/docs/contributing/`.

- Keep each PR focused. Explain the trigger, resulting behavior, validation and known limits.
- Run `gofmt` for Go and Prettier for TypeScript/MDX. Check both Go modules when affected.
- Use isolated Agent fixtures, temporary data and local Worker resources. Never test by changing real user sessions.
- UI changes include desktop and narrow-screen screenshots; protocol changes update Go and TypeScript together.
- Declare only implemented Provider capabilities, and update the Agent support matrix.
- Do not commit binaries, local databases, keys or private transcripts. Follow [SECURITY.md](SECURITY.md) for vulnerabilities.

Web/Worker: `pnpm check`, `pnpm test`, `pnpm build:web`. Go: `go vet ./...`, `go test ./...` at the repository root and under `clients/nsgo`. Native UI tests require a suitable platform/toolchain. Platform build coverage is enforced by GitHub Actions.

By contributing, you confirm you have the right to contribute your changes under this repository's MIT license.
