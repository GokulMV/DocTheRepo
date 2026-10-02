# Contributing to DocTheRepo Hub

Thanks for helping. Bug reports, ideas, docs fixes and code are all welcome.

## Ways to contribute

- **Report a bug or ask for a feature:** open an issue with the templates. For bugs, include the version
  (`dth --version`, or the hub's start-up log line), what you did, what you expected, and what happened. If
  the UI showed an error card, paste its **Details for a bug report**.
- **Security issues:** never in public issues. See [SECURITY.md](SECURITY.md).
- **Pull requests:** for anything beyond a small fix, open an issue first, so we can agree on the approach
  before you spend time on it.

## Development setup

You need Go (the version in `go.mod`), Node.js 22.12+ (or 20.19+), and Docker (integration tests start
PostgreSQL + pgvector with testcontainers). `./scripts/quickstart.sh` installs all of these for you.

```sh
make test        # Go unit + integration tests (needs Docker)
make web-test    # UI typecheck + unit tests
make e2e         # Playwright end-to-end tests against the real hub and mocks
make lint        # go vet (CI also runs gofmt, npm audit and govulncheck)
make security    # govulncheck + npm audit
```

More detail is in [docs/development.md](docs/development.md); how the code is organised is in
[docs/architecture.md](docs/architecture.md).

## Guidelines

- **Architecture:** ports and adapters. Core logic (`internal/core`) depends only on the interfaces in
  `internal/ports`; anything that talks to the outside world is an adapter in `internal/adapters`.
- **Tests come with the change:** unit tests for logic, an integration test against Postgres when you touch
  the store or an API, and a UI test for UI behaviour. A bug fix starts with a test that reproduces it.
- **Database:** add a new numbered migration in `migrations/` (with a `.down.sql`). Never edit an existing
  one. Change SQL in `internal/store/queries` and regenerate with `sqlc generate`.
- **Secrets:** never log, return, audit or export secret values. Secrets are write-only (see
  [docs/security.md](docs/security.md)).
- **Dependencies:** prefer the standard library. Any new dependency needs a reason in the PR and a
  permissive license; run `make licenses` to refresh [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
- **Style:** `gofmt`, and match the surrounding code. Comments explain why, not what. User-facing text is
  plain and specific.
- **Commits:** small and focused, with a subject line that says what changed and a body that says why.

## Pull request checklist

- [ ] Tests added or updated, and `make test`, `make web-test` pass locally
- [ ] Docs updated (`docs/`, and the OpenAPI list in `internal/api/openapi.go` for API changes)
- [ ] No secrets, credentials or personal data in code, tests or fixtures
- [ ] [CHANGELOG.md](CHANGELOG.md) updated under **Unreleased** for user-visible changes

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE), and you
agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
