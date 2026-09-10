# AGENTS.md — mcp-forgejo

Operational guidance for AI agents working in this repository. **Read `SPEC.md`
first** — it is the normative specification for the server's architecture, tool
surface, security model, logging, and CI/CD. This file tells you *how to work*
in the repo.

## Language

- All documentation — `SPEC.md`, `AGENTS.md`, `README.md` — is **English**. These
  three files are **strictly English**; do not introduce other languages.
- Commit messages and code comments are **English**.

## What this repo is

`mcp-forgejo` is a **Hybrid** Go MCP server that wraps the **Forgejo REST API**
(local `stdio` + remote `HTTP/SSE`). It talks only to the remote Forgejo
instance over HTTP; it **never touches the local filesystem** (so there is **no
`ALLOW_DIRS`** — do not add it). See `SPEC.md` §3 and §5.

## Hard facts you must not change

- **Language:** Go **1.27.0** (`go.mod` `go 1.27.0`; `GO_VERSION: "1.27"` in CI).
- **Module path:** `example.com/teran/mcp-forgejo` — a **placeholder** for the
  internal-only path (S6/N20). The real upstream location is kept private; do
  not rewrite it to a real public/external domain such as `github.com/...`.
- **SDK:** `github.com/modelcontextprotocol/go-sdk` (official; never hand-roll).
- **Logger:** `logrus` only. Channel per transport (L1): HTTP/SSE → stdout;
  stdio → file (`/tmp/mcp-forgejo.log`, chmod 600), **never** stdout.
- **Default branch:** `master` (never `main`).
- **Auth:** single Forgejo PAT via `Authorization: token <PAT>` from
  `FORGEJO_TOKEN`. **No OAuth2.**

## TDD workflow (T1)

All features and fixes are **test-first**, in isolated contexts:

1. **`@qa`** writes the tests (isolated context).
2. **`@developer`** writes the implementation (isolated context).

Do not write implementation before tests for a new behavior. Tests must keep
total coverage **≥ 95%** and leave **no gremlins survivors**.

## Tooling commands

```bash
golangci-lint run ./...                 # lint + gofmt/gofumpt; findings fixed
go vet ./...                            # go vet
go test -race ./...                     # unit tests with race detector
go test -coverprofile=cover.out -covermode=atomic ./...
go tool cover -func=cover.out | awk '/^total:/ {print $3}'   # must be >= 95%
gosec ./...                             # static security; findings FIXED, not suppressed
govulncheck ./...                       # vuln audit; findings FIXED, not suppressed
go-arch-lint check                      # dependency rules (.go-arch-lint.yml)
gremlins unleash . --threshold-efficacy=90 --threshold-mcover=0  # HARD GATE
```

## Rules

- **Coverage gate:** total coverage < **95%** fails the build. Keep it above.
- **Security:** never commit, log, echo, or surface `FORGEJO_TOKEN` or any
  credential. Redact it from outputs and logs (S2). Do **not** add blanket
  `#nosec` / default excludes — **fix, don't suppress** (S5/N8).
- **TLS:** never implement TLS inside the server; it is the reverse proxy's job
  (S1/N1).
- **Architecture:** respect `.go-arch-lint.yml` edges — `cmd` → `internal`,
  `domain` is pure, `application`/`infrastructure` depend only on `domain`,
  `server` binds them; `config`/`logging` are leaves. `go-arch-lint check` must
  pass.
- **Tool surface:** add/modify tools per `SPEC.md` §6. Every tool needs the five
  annotations + per-tool instructions, is a **complete use case**, and maps to
  Forgejo `operationId`s. Group new tools read → write → delete.
- **Gremlins:** mutation testing is a **hard gate** — survivors fail the build.
  If a mutation survives, your tests are too weak; strengthen them. Never set
  `continue-on-error` on the gremlins job. The efficacy threshold **must be > 0**
  (`gremlins unleash . --threshold-efficacy=90 --threshold-mcover=0`): with
  `--threshold-efficacy=0` gremlins 0.6.x never fails on survivors (its `assess()`
  only fails when the threshold is positive), which would make the gate a paper
  gate. Run on the module root `.`, not `./...` (which finds no mutants in
  0.6.x). Keep the threshold above the real efficacy so a living survivor drops
  the score below it and fails the build.

## Build / image workflow (Hybrid)

The server builds a static binary and a container image (required because it
supports HTTP/SSE — R1).

```bash
# local build
go build -o mcp-forgejo ./cmd/mcp-forgejo

# run locally (stdio)
FORGEJO_URL=https://git.example.com FORGEJO_TOKEN=<pat> ./mcp-forgejo --transport=stdio

# run as a remote sidecar (HTTP/SSE)
FORGEJO_URL=https://git.example.com FORGEJO_TOKEN=<pat> \
  HOST=0.0.0.0 PORT=8080 ./mcp-forgejo --transport=http-sse
```

- **Image build:** the binary is produced by **GoReleaser** (`.goreleaser.yaml`)
  and the `Dockerfile` (distroless runtime, `EXPOSE 8080`) only copies it in —
  it does NOT recompile. Build locally:
  ```bash
  goreleaser build --snapshot --clean --single-target --output dist/mcp-forgejo
  docker build -t mcp-forgejo .
  ```
- **CI publish:** `.github/workflows/images.yml` runs a GoReleaser build step then
  builds/publishes the image on every git tag (R3 tags) and every `master`
  commit (R4 tags) to `ghcr.io/teran/mcp-forgejo`.

## Pointers

- **Spec:** `SPEC.md` — architecture (§4), security (§5), tools (§6), logging
  (§7), CI (§8), conformance (§10).
- **CI:** `.github/workflows/ci.yml` (quality gates), `.github/workflows/images.yml`
  (image publishing).
- **Config:** `.go-arch-lint.yml`, `.golangci.yml`.
