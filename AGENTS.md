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

- **Language:** Go **1.27.1** (`go.mod` `go 1.27.1`; `GO_VERSION: "1.27"` in CI).
- **Module path:** `github.com/teran/mcp-forgejo` (matches the canonical public
  repository location). Keep it in sync with the repository.
- **SDK:** `github.com/modelcontextprotocol/go-sdk` (official; never hand-roll).
- **Logger:** `logrus` only. Channel per transport (L1): HTTP/SSE → stdout;
  stdio → file (`/tmp/mcp-forgejo.log`, chmod 600), **never** stdout.
- **Default branch:** `master` (never `main`).
- **Auth:** single Forgejo PAT. Per-transport: **HTTP/SSE** accepts the token
  per-request from the `Authorization: Bearer <token>` header (remote auth via
  headers, M6; requests without a valid Bearer token get `401`); **stdio**
  requires `FORGEJO_TOKEN` env (local auth via env). The resolved token is sent
  outbound as `Authorization: token <PAT>` on every Forgejo call. **No OAuth2.**
- **Observability (Hybrid/Remote):** HTTP/SSE mode **always** starts the internal
  observability listener on `INTERNAL_ADDR` (default `:8081`) — `/metrics` (standard
  Go collectors), `/debug/pprof/*`, `/healthz`, `/readyz` — with **no opt-out**
  (O1/O4/N32). It is **not** started in stdio mode. The MCP listen address is
  `HOST:PORT` (default `0.0.0.0:8080`); there is no `LISTEN_ADDR`. In addition to the
  standard Go collectors, upstream Forgejo metrics are exposed on `/metrics` (O3:
  `forgejo_upstream_request_duration_seconds`, `forgejo_upstream_request_total`,
  `forgejo_upstream_request_size_bytes`, `forgejo_upstream_response_size_bytes`),
  observed once per outbound request. See SPEC.md §7.1.

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
gitleaks detect --source . --redact --verbose   # secret scan over git history (full clone); findings FIXED
go-arch-lint check                      # dependency rules (.go-arch-lint.yml)
gremlins unleash . --threshold-efficacy=90 --threshold-mcover=80 --timeout-coefficient=60  # HARD GATE
make e2e                                # e2e via go-docker-testsuite (build tag `e2e`); needs Docker — HARD GATE in CI
```

> **Gremlins `--threshold-mcover` = 80 (goal reached) with
> `--timeout-coefficient=60`.** gremlins 0.6.x derives its per-mutant timeout
> from the baseline suite time (default coefficient 3), so on a warm Go test
> cache (the CI-equivalent run) the short timeout made most mutants report
> "Timed out" and excluded them from the coverage/efficacy accounting — capping
> reported mutant coverage near 21–35%. Raising the coefficient to 60 lets every
> mutant actually run (0 timed out), giving **mcover ≈ 83%** with **efficacy
> ≈ 97%** (≥ 90). The remaining ~6 live mutants are near-equivalent
> (time-constant arithmetic and `len(query) > 0` / `err != nil` boundary cases)
> needing production refactoring to kill. gremlins 0.6.x warm-cache coverage
> accounting is fragile (it can report NOT COVERED for lines `go tool cover`
> shows at 100%), so reported mcover varies run-to-run; 80 is set against the
> ~83% warm-cache measurement. See SPEC.md §8.

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
  (`gremlins unleash . --threshold-efficacy=90 --threshold-mcover=30`): with
  `--threshold-efficacy=0` gremlins 0.6.x never fails on survivors (its `assess()`
  only fails when the threshold is positive), which would make the gate a paper
  gate. Run on the module root `.`, not `./...` (which finds no mutants in
  0.6.x). Keep the threshold above the real efficacy so a living survivor drops
  the score below it and fails the build. Always pass `--timeout-coefficient=60`
  so every mutant is actually exercised on a warm cache (see the mcover note
  above).

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
