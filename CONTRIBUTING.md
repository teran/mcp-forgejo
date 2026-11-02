# Contributing

Thanks for your interest in contributing! Please read this guide and the
repository's `AGENTS.md` and `SPEC.md` before opening a pull/merge request.

> `SPEC.md` and `AGENTS.md` are strictly English; the README is English by
> default unless you explicitly request another language.

## How to contribute

- **Report a bug** — open an issue. Include the exact command/arguments, the
  expected and actual behaviour, and (if relevant) logs **without secrets**
  (see the redaction rules in `AGENTS.md` — `FORGEJO_TOKEN` and any
  credentials must never be echoed).
- **Request a feature** — open an issue describing the use case, who calls the
  server, and the external system it would talk to (this drives the transport
  and auth decisions, see `SPEC.md` §1–§2).
- **Open a pull/merge request** — describe the change and link the related
  issue. Every change must keep CI green (see below). The default branch is
  `master`; base your work on it.

## Development workflow

Work is done **test-first** (TDD, `SPEC.md` §9):

1. **`@qa`** writes the tests first, using an **isolated context**.
2. **`@developer`** writes the implementation, using an **isolated context**.

Do not write implementation before tests for a new behaviour, and do not
weaken tests to make an implementation pass.

## Quality gates (CI enforces these)

CI runs the checks from `AGENTS.md` / `SPEC.md` §8:

```bash
golangci-lint run ./...                 # lint + gofmt/gofumpt
go vet ./...                            # go vet
go test -race ./...                     # unit tests with race detector
go test -coverprofile=cover.out -covermode=atomic ./...   # coverage gate
gosec ./...                             # static security; findings FIXED
govulncheck ./...                       # vuln audit; findings FIXED
go-arch-lint check                      # dependency rules (.go-arch-lint.yml)
make e2e                                # e2e via go-docker-testsuite (build tag `e2e`)
```

- **Coverage ≥ 95%** — total coverage below 95% fails the build. Add tests with
  the code.
- **Mutation testing** (gremlins) — a **hard gate** at an 80% mutant-coverage
  floor with efficacy ≥ 90%. Weak tests are a defect; improve them, don't lower
  the threshold or set `continue-on-error`.
- **gosec / govulncheck / go-arch-lint / gitleaks** — findings are **fixed,
  not suppressed**. Never blanket-suppress a security finding or add default
  `#nosec` excludes.
- **e2e tests** — exercise the full path between the MCP tool handler and a
  real Forgejo backend; they are build-tagged (`//go:build e2e`) and gated in a
  dedicated CI job. `make e2e` requires a running Docker daemon.

## Definition of done

A change is done only when CI is green: lint, `-race` tests with coverage
≥ 95%, gosec, govulncheck, go-arch-lint, the gremlins mutation gate, and the
`e2e` job.

## Reporting security issues

Please do **not** open a public issue for a security vulnerability — see
[SECURITY.md](SECURITY.md) for the responsible-disclosure process.

## Code of Conduct

This project adheres to the [Code of Conduct](CODE_OF_CONDUCT.md). By
participating you are expected to uphold this code.
