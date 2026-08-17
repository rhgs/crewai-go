# Contributing to crewai-go

> **Languages:** **English** (current) · [Português](CONTRIBUTING.pt-BR.md)

Thanks for your interest in contributing. This project aims to stay small,
auditable, and dependency-free while remaining useful for real multi-agent
workloads.

## Code of Conduct

By participating, you agree to uphold our [Code of Conduct](CODE_OF_CONDUCT.md)
([PT](CODE_OF_CONDUCT.pt-BR.md)).

## Ways to contribute

- Report bugs and request features via [GitHub Issues](https://github.com/rhgs/crewai-go/issues)
- Improve documentation (English and Portuguese mirrors)
- Add examples, tests, or small, focused features
- Review pull requests

Please open an issue before large design changes so we can align on scope.

## Development setup

Requirements:

- Go **1.24+**
- `git`

```bash
git clone https://github.com/rhgs/crewai-go.git
cd crewai-go
go test ./...
```

No external module dependencies are required (`go.mod` is stdlib-only).

## Project conventions

| Rule | Detail |
|------|--------|
| Zero external dependencies | Do not add entries to `go.mod` unless discussed and justified. |
| English in code | Godoc, identifiers, error messages, and prompts are English (no accents). |
| Bilingual docs | User-facing docs (`README`, `docs/`, plans, changelogs) keep EN + PT-BR mirrors. |
| Sentinel errors | Prefer package-level `var Err… = errors.New(...)` and `errors.Is`. |
| Context on I/O | Propagate `context.Context` through every LLM, tool, and network call. |
| Functional options | Prefer `WithX(...)` options for optional configuration. |
| Hermetic tests | Use `llm/mock` and local fakes; no real network in unit tests. |
| Race safety | Providers and shared types must be safe for concurrent use; CI runs `-race`. |

## Workflow

1. Fork the repository (or create a branch if you have write access).
2. Create a focused branch: `feat/…`, `fix/…`, `docs/…`.
3. Make small, reviewable commits.
4. Keep the default behavior backward compatible unless the PR is an intentional break.
5. Update docs and CHANGELOG when user-visible behavior changes.
6. Open a pull request against `main` and fill in the PR template.

### Local checks (must pass)

```bash
gofmt -l .
go vet ./...
go build ./...
go test -race ./...
go test -cover ./...
```

CI runs the same gates on every pull request.

### Coverage

CI measures statement coverage across library packages only (`go list ./...`
excluding `/examples`). The project gate is **≥ 90%** total. New code should
ship with tests. Prefer table-driven tests and the mock LLM for multi-phase
agent flows.

## Pull request checklist

- [ ] `gofmt`, `go vet`, `go build`, and `go test -race` are clean
- [ ] Tests cover the new behavior (including failure paths)
- [ ] Docs updated (EN + PT-BR when applicable)
- [ ] CHANGELOG entry under the unreleased / next version section when relevant
- [ ] No secrets, API keys, or credentials in the diff
- [ ] PR description explains *why*, not only *what*

## Reporting security issues

Do **not** open a public issue for vulnerabilities. Follow [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE) that covers this project.
