# mcp-forgejo — SPEC

**Purpose.** `mcp-forgejo` is a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that wraps the **Forgejo REST API** so a model-driven client (editor, CLI, or remote team sidecar) can perform common software-development tasks against an internal Forgejo instance: repositories, files/contents, issues, pull requests, branches, releases, and organizations.

**Language (G2).** Go, pinned at the latest stable **1.27.0** (see `go.mod`, `GO_VERSION: "1.27"` in `ci.yml`) — see [G1]. The official MCP SDK `github.com/modelcontextprotocol/go-sdk` is used; the protocol is **never hand-rolled** (M1).

**Deployment type (S6).** **HYBRID** — supports both **STDIO** (local editor/CLI companion) and **HTTP/SSE** (remote sidecar). Because it supports HTTP/SSE, it MUST build & publish a container image (R1) and route logs to the channel appropriate per transport (L1). Under DDD/Clean this is achieved with **driver implementations per launch mode** (a `stdio` driver and an `http/sse` driver), selected at startup.

> The implementation of the Go application logic is **not** part of this document's scope. This SPEC defines the contract, architecture, tool surface, security model, logging, and CI/CD so that the scaffold can be built against a fixed specification.

---

## 1. Transport decision (M2 / N10)

The server is **HYBRID: STDIO + HTTP/SSE**. The transport is chosen based on the two distinct ways this server is used:

- **Local editor/CLI companion → STDIO.** The most common MCP integration is a local client (editor, terminal, agent harness) that spawns the server as a child process and speaks MCP over its standard input/output. This needs **no listener and no open ports**, is short-lived per session, and is the simplest, most secure local deployment. Over stdio, the **stdout channel is reserved for the MCP protocol**, so logs go to a file (L1).
- **Remote/team sidecar → HTTP/SSE.** For a long-running, always-on deployment serving multiple concurrent sessions (a shared team gateway, a container, a Kubernetes sidecar behind a reverse proxy), a stateless HTTP endpoint with Server-Sent Events is required. This supports remote clients, concurrency, and standard deployment tooling.

Both transports are driven by the **same tool registry and application layer**; only the transport driver and the logging sink differ. This gives a single binary that is useful both locally and remotely.

---

## 2. Auth decision (M3 / N10) — OAuth2 is NOT used

**OAuth2 is not used.** The server authenticates to Forgejo with a single **personal access token (PAT)** supplied via environment, sent as an `Authorization: token <PAT>` header on every Forgejo API call.

**Why no OAuth2:** the target is an **internal** Forgejo instance, and the Forgejo swagger exposes **no OAuth2 client flow** for the operations this server performs — there is no authorization-code or client-credentials endpoint usable here. A single PAT is the natural, minimal mechanism: it is scoped to the calling user, long-lived, trivially issued/revoked in the Forgejo UI, and sufficient for both launch modes:

- **stdio (local):** the PAT is supplied to the server by the local operator via `FORGEJO_TOKEN` in the environment — the standard local-credential path.
- **HTTP/SSE (remote):** the same PAT is injected into the container/pod environment by the operator or deployment system (secret, config map, or secret manager). The MCP client itself does not need credentials; the server holds the PAT.

OAuth2 would be warranted only if the server had to **delegate authorization to untrusted remote clients** (token exchange with the MCP SDK). That is not the case: `mcp-forgejo` is a server-side client of Forgejo, not an identity provider, and it serves a closed, trusted team. Hence **PAT, no OAuth2** (S2 data-hygiene rules apply to the token; see §5).

---

## 3. Configuration (A1)

Configuration is loaded from the environment with [`kelseyhightower/envconfig`](https://github.com/kelseyhightower/envconfig) into a single `Config` struct (`internal/config`).

| Env var          | Type       | Default            | Description |
|------------------|------------|--------------------|-------------|
| `FORGEJO_URL`    | `string`   | (from env)         | Base URL of the Forgejo instance, e.g. `https://git.homelab.teran.dev`. Required. |
| `FORGEJO_TOKEN`  | `string`   | (empty)            | Forgejo **personal access token** (PAT). **Secret** — never logged, echoed, or included in tool output (S2/N2). Required for authenticated operations. |
| `HOST`           | `string`   | `0.0.0.0`          | Listen host for the HTTP/SSE transport. |
| `PORT`           | `string`   | `8080`             | Listen port for the HTTP/SSE transport. |
| `LOG_LEVEL`      | `string`   | (unset)            | Logging level (`trace`, `debug`, `info`, `warn`, `error`). **Unset ⇒ logging disabled** (L2). |
| `LOG_FILENAME`   | `string`   | `/tmp/mcp-forgejo.log` | Log file path for the **stdio** transport (chmod **600**). Ignored for HTTP/SSE (L1, L3). |
| `LOG_FORMAT`     | `string`   | `text`             | `text` (logrus text, full absolute timestamp) or `json` (L4). |

**`ALLOW_DIRS` is intentionally absent.** The server **never touches the local filesystem** — every operation is an HTTP call to the remote Forgejo API (S4 applies only when the server touches the local filesystem; here it does not). Files are read/written **on the Forgejo server**, not on the machine running `mcp-forgejo`. Therefore no directory scoping is required, and `ALLOW_DIRS` is omitted from config and CLI. This is stated explicitly here so reviewers do not expect it.

---

## 4. Architecture (A1 / DDD / Clean)

The application is layered with Clean / DDD principles. The **composition root** wires concrete dependencies; the core depends on interfaces.

### 4.1 Package layout

```
cmd/mcp-forgejo/
  main.go                # composition root: load config, build logging, construct
                         # Forgejo client + tool registry, select transport driver, serve.
internal/
  domain/                # pure entities + interfaces (Repo, File, Issue, PullRequest,
                         # Release, Organization, Comment, Branch, Commit). No deps.
  application/           # use cases / tool handlers implementing domain interfaces.
  infrastructure/
    forgejo/             # Forgejo REST client (net/http) implementing the domain
                         # interfaces; auth header, pagination, error mapping.
  server/                # MCP tool registry + transport drivers (stdio, http/sse).
  config/                # envconfig Config struct + load/validate.
  logging/               # logrus setup, sink per transport, redaction helpers.
```

### 4.2 Tool registry

The **tool registry** (`internal/server`) is a single map of tool name → `Tool` where each `Tool` carries:

- the **metadata** (title, annotations, per-tool instructions, input/output JSON Schema — see §6),
- a **handler** function that invokes the corresponding **application use case**,
- its **priority group** (read / write / delete) used to order registration (S3).

At startup the registry is populated once, then bound to the MCP SDK `Server` (via `server.RegisterTool(...)` from the official go-sdk). The same registry is used by both transport drivers, guaranteeing identical tool surfaces over stdio and HTTP/SSE.

### 4.3 Transport wiring

Transport selection happens at startup, in the composition root:

- **stdio driver** (`internal/server/stdio.go`) — connects the SDK server to `os.Stdin`/`os.Stdout`. Logging sink = **file** (`LOG_FILENAME`, chmod 600).
- **http/sse driver** (`internal/server/http.go`) — serves the MCP HTTP/SSE endpoint on `HOST:PORT`. Logging sink = **stdout** (12-factor).

A single launch-mode flag selects the driver (e.g. `--transport=stdio|http-sse`, defaulting sensibly per how it is launched). Both drivers share the same `Server` construction, tools, and application layer.

### 4.4 Error handling (taxonomy → MCP error codes)

Forgejo responses are mapped to a small error taxonomy in `internal/infrastructure/forgejo`, then to MCP error codes:

| Taxonomy            | Trigger (Forgejo HTTP)                       | MCP error code / handling |
|---------------------|----------------------------------------------|---------------------------|
| `validation`        | 422 Unprocessable Entity, 409 Conflict       | `INVALID_PARAMETERS` — surface a friendly message; no retry. |
| `notFound`          | 404 Not Found                                | `NOT_FOUND` / tool returns a clear "not found" result. |
| `forbidden`         | 403 Forbidden, 423 repoArchived              | `ACCESS_DENIED` — scoped; never leaks token details. |
| `unauthorized`      | 401 Unauthorized                             | `ACCESS_DENIED` — token missing/revoked; log a generic message (never the token). |
| `transient`         | 5xx, timeouts, network                       | `INTERNAL_ERROR` / `RESOURCE_BUSY` — safe to retry with backoff. |

Errors are **logged** at an appropriate level (transient at warn, others at info/debug) and surfaced in the tool result with **no** sensitive data (S2).

### 4.5 Dependency boundaries

`go-arch-lint` enforces the edges in `.go-arch-lint.yml`:

- `cmd` → any `internal/*` (composition root).
- `domain` → nothing (pure core).
- `application` → `domain` only.
- `infrastructure` → `domain` only.
- `server` → `domain`, `application`, `infrastructure` (the adapter that binds layers).
- `config`, `logging` → nothing internal.

Layers communicate through **domain interfaces**; `application` and `infrastructure` never import each other directly.

---

## 5. Security (S1–S6)

- **S1 / N1 — TLS never in-server.** TLS is **NEVER implemented inside the server** for the HTTP/SSE transport. The HTTP/SSE listener serves plain HTTP; TLS termination is always the **reverse proxy's** job (nginx / Caddy / ingress) in front of the container. No in-server TLS code, certificates, or key handling exists or is planned.
- **S2 / N2 — data hygiene / redaction.** The `FORGEJO_TOKEN` (and any credentials/passwords) are **never** echoed in logs, tool outputs, error messages, or debug dumps. The PAT is read into config once and used only as the outbound `Authorization: token <PAT>` header. Tool output contracts contain no token field; any server string that could embed credentials is redacted by a shared helper before being returned or logged.
- **S3 — tool priority order.** Tools are grouped and registered **read → write/update → delete** (see §6).
- **S4 — no local filesystem access.** The server does not touch the local filesystem; it only talks to the remote Forgejo API over HTTP. Consequently **`ALLOW_DIRS` is not required and is omitted** from config (see §3). There is no local path to scope, so N3 is vacuous by design.
- **S5 / N8 — fix, don't suppress.** gosec and govulncheck findings are **fixed**, never suppressed via blanket exclusions or default `#nosec`. Findings block the build (C4/C5).
- **S6 / N20 — local-only module name.** `mcp-forgejo` is hosted on an internal Forgejo. The module/package name is **local-only** — `git.homelab.teran.dev/teran/mcp-forgejo` — and MUST NOT reference a public/external domain such as `github.com/...`. This keeps the real upstream location private as a **security measure**.
- **S7 — reverse-proxy authn/authz for HTTP/SSE.** The MCP HTTP/SSE layer offers **no authentication of its own**: the server holds the PAT in its environment and uses it to call Forgejo, but any client that can reach `HOST:PORT` can drive every tool with the operator's privileges — including destructive ones (`forgejo_repo_delete`, `forgejo_pull_merge`, …). For any HTTP/SSE deployment the listener **MUST** sit behind a reverse proxy that enforces client authentication/authorization (e.g. mTLS, OIDC, network ACL, or a proxy token) before requests reach the server. Additionally, run the PAT under a **dedicated low-privilege Forgejo user** scoped to only the operations the team actually needs. In **stdio** mode the client is trusted by construction (the local process that spawned the server), so this does not apply.

---

## 6. Tool surface (M4, M5, S3)

All tools target the **closed domain** of the configured Forgejo instance (`openWorldHint: false`). Every tool is a **complete use case** (M5): a client accomplishes a finished task in a single call without chaining low-level primitives. Each tool maps to one or more Forgejo `operationId`s from the Forgejo swagger.

**Common request parameters:** `owner`, `repo`, `ref`/`branch`, pagination `page` (1-based) + `limit`. **Common errors:** 401, 403, 404, 422, 409, 423. Content in file operations is base64-encoded over the wire; bodies carry commit metadata (message, branch, `new_branch`, author).

### 6.0 Metadata conventions

- **read** tools → `readOnlyHint: true`, `destructiveHint: false`, `idempotentHint: true`, `openWorldHint: false`.
- **write/update** tools → `readOnlyHint: false`, `destructiveHint: false`, `openWorldHint: false`; `idempotentHint: true` only where repeating with identical args has no extra effect.
- **delete** tools → `readOnlyHint: false`, `destructiveHint: true`, `idempotentHint: false`, `openWorldHint: false` (deleting an already-removed resource typically errors, so delete is **not** idempotent).

Representative JSON-Schema-style metadata block (shown for one tool; all tools carry the same five annotations + instructions):

```json
{
  "name": "forgejo_file_get",
  "title": "Read file",
  "description": "Returns the UTF-8 text content of a file at path+ref, plus commit/sha metadata.",
  "annotations": {
    "title": "Read file",
    "readOnlyHint": true,
    "destructiveHint": false,
    "idempotentHint": true,
    "openWorldHint": false
  },
  "instructions": "Use to read a single file's content from the configured Forgejo repo. Provide the file path and an optional ref (branch/tag/sha); defaults to the default branch. Returns base64-decoded UTF-8 text plus the blob sha and commit metadata. If the blob is not valid UTF-8 (a binary file such as an image or archive), the tool does NOT return corrupted text — it returns a binary flag instead of decoding. Never attempts to read local filesystem paths.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "owner": { "type": "string", "description": "Repository owner/namespace" },
      "repo": { "type": "string", "description": "Repository name" },
      "path": { "type": "string", "description": "File path in the repo" },
      "ref": { "type": "string", "description": "Branch/tag/sha; defaults to default branch" }
    },
    "required": ["owner", "repo", "path"]
  }
}
```

### 6.1 READ — `readOnlyHint: true`, `openWorldHint: false`

| # | Tool | Title | Forgejo operationId(s) | Instructions (abridged) |
|---|------|-------|------------------------|--------------------------|
| 1 | `forgejo_repo_search` | Search repositories | `repoSearch` | Search the Forgejo instance by `q`, `topic`, `sort`, `order`, `private`. Returns matching repos (id, full name, visibility, default branch, description). `idempotentHint: true`. |
| 2 | `forgejo_repo_get` | Get repository | `repoGet`, `repoGetByID` | Return details of one repo by owner+name (or id). Read-only; never modifies. |
| 3 | `forgejo_org_list` | List my organizations | `orgListCurrentUserOrgs` | List the organizations the current user belongs to. Read-only. |
| 4 | `forgejo_repo_list_contents` | List directory contents | `repoGetContentsList` | List entries (type/sha/size) in a directory at `path`+`ref`. Read-only. |
| 5 | `forgejo_file_get` | Read file | `repoGetContents`, `repoGetRawFile` | Return base64-decoded UTF-8 content of a file at `path`+`ref`, plus commit/sha metadata (see block above). Read-only. |
| 6 | `forgejo_diff_get` | Get diff | `repoCompareDiff`, `repoDownloadPullDiffOrPatch` | Return a unified diff between two refs (`basehead`) or for a PR, as text. Read-only. |
| 7 | `forgejo_commit_list` | List commits | `repoGetAllCommits` | List commits of a repo branch with pagination. Read-only. |
| 8 | `forgejo_branch_list` | List branches | `repoListBranches` | List branches of a repo. Read-only. |
| 9 | `forgejo_issue_list` | List issues | `issueListIssues` | List issues with a `state` filter (`open`/`closed`/`all`) + pagination. Read-only. |
| 10 | `forgejo_issue_get` | Get issue + comments | `issueGetIssue`, `issueGetComments` | Single call returning an issue **and** its comments. Read-only. |
| 11 | `forgejo_pull_list` | List pull requests | `repoListPullRequests` | List PRs with a `state` filter + pagination. Read-only. |
| 12 | `forgejo_pull_get` | Get pull request + files + checks | `repoGetPullRequest`, `repoGetPullRequestFiles`, `repoGetCombinedStatusByRef` | Single call returning a PR **with** its changed files **and** combined checks status. Read-only. |
| 13 | `forgejo_release_list` | List releases | `repoListReleases`, `repoGetLatestRelease` | List releases of a repo, or fetch the latest. Read-only. |

### 6.2 WRITE / UPDATE — `readOnlyHint: false`, `destructiveHint: false`

| # | Tool | Title | Forgejo operationId(s) | `idempotent` | Instructions (abridged) |
|---|------|-------|------------------------|--------------|--------------------------|
| 14 | `forgejo_repo_create` | Create repository | `createCurrentUserRepo` | false | Create a repo (`name`, optional `owner`/org, `private`, `auto_init`). Creating a name that already exists **conflicts** — not idempotent. |
| 15 | `forgejo_file_write` | Write/update file | `repoCreateFile`, `repoUpdateFile` | false | **Single call covering create AND update**: write `content` (text, base64-encoded on the wire) at `path`+`branch` with a commit `message`. Creates if absent, updates if present. Not idempotent: every call records a new commit (the blob sha changes), even for identical content. |
| 16 | `forgejo_file_write_many` | Write multiple files | `repoChangeFiles` | false | Modify several files in **one commit** (multi-file single commit) at a branch with a commit message. |
| 17 | `forgejo_branch_create` | Create branch | `repoCreateBranch` | false | Create a branch from an existing ref. Creating an existing branch **conflicts**. |
| 18 | `forgejo_issue_create` | Create issue | `issueCreateIssue` | false | Create an issue with `title`, `body`, `labels`, `milestone`. |
| 19 | `forgejo_issue_update` | Update/close/reopen issue | `issueEditIssue` | true | **Single tool handles edit + close + reopen**: update fields; set `state` to `open`/`closed`. Repeating the same edit is idempotent. |
| 20 | `forgejo_issue_comment_add` | Add issue comment | `issueCreateComment` | false | Append a comment to an issue. Each call adds a new comment. |
| 21 | `forgejo_pull_create` | Create pull request | `repoCreatePullRequest` | false | Open a PR from `head`→`base` with `title`/`body`. |
| 22 | `forgejo_pull_update` | Update/close/reopen PR | `repoEditPullRequest` | true | **Single tool handles edit + close + reopen**: update fields; set `state`. |
| 23 | `forgejo_pull_merge` | Merge pull request | `repoMergePullRequest`, `repoPullRequestIsMerged` | false | Merge a PR with a `merge`/`squash`/`rebase` method and report the result (merged vs already-merged). |
| 24 | `forgejo_pull_review` | Review pull request | `repoCreatePullReview`, `repoSubmitPullReview` | false | **Create AND submit** a PR review (`approve`/`comment`/`request_changes`) in one call. |
| 25 | `forgejo_release_create` | Create release | `repoCreateRelease` | false | Create a release for an existing `tag` with `title`/`notes`. |

### 6.3 DELETE — `readOnlyHint: false`, `destructiveHint: true`

| # | Tool | Title | Forgejo operationId(s) | Instructions (abridged) |
|---|------|-------|------------------------|--------------------------|
| 26 | `forgejo_file_delete` | Delete file | `repoDeleteFile` | Delete a file at `path`+`branch` with a commit `message`. **Destructive** — confirm before use. |
| 27 | `forgejo_branch_delete` | Delete branch | `repoDeleteBranch` | Delete a branch. **Destructive** — confirm before use; will not delete the default branch. |
| 28 | `forgejo_issue_delete` | Delete issue | `issueDelete` | Permanently delete an issue. **Destructive** — confirm before use. |
| 29 | `forgejo_comment_delete` | Delete comment | `issueDeleteComment` | Delete an issue/PR comment. **Destructive** — confirm before use. |
| 30 | `forgejo_release_delete` | Delete release | `repoDeleteRelease` | Delete a release (tag remains). **Destructive** — confirm before use. |
| 31 | `forgejo_repo_delete` | Delete repository | `repoDelete` | **Permanently delete a repository.** Highly destructive — instructions require explicit user confirmation before invoking. |

> **Data-hygiene (S2):** none of the above tools accept or return a token/credential. Auth is injected server-side from `FORGEJO_TOKEN`; outputs never contain it.

---

## 7. Logging (L1–L5, G8)

Uses **`logrus`** (`github.com/sirupsen/logrus`).

- **L1 — channel per transport:** HTTP/SSE → **stdout** (12-factor); stdio → **file** (default `/tmp/mcp-forgejo.log`, **chmod 600**), **never** stdout (stdout is the MCP protocol channel).
- **L2 — disabled by default:** logging is enabled only when `LOG_LEVEL` is set; unset ⇒ no logs.
- **L3 — override path:** `LOG_FILENAME` (default `/tmp/mcp-forgejo.log`).
- **L4 — format:** default `text` (logrus text, **full absolute timestamp**); `LOG_FORMAT=json` → JSON.
- **L5 / S2 / N2 — no secrets:** `FORGEJO_TOKEN` and any credentials/passwords are **never** logged; redaction helpers strip them from any log line or error before emission.

---

## 8. CI & tooling (C1–C8, G1, R1–R4)

The Go profile is enforced in CI (`.github/workflows/ci.yml`) and locally:

| Check | Command | Gate |
|-------|---------|------|
| Go version | `go 1.27.0` in `go.mod` == `GO_VERSION: "1.27"` | G1/N7 |
| Coverage | `go test -race -coverprofile=cover.out -covermode=atomic ./...` + total **≥ 95%** | C1/N6 (build fails below 95) |
| Race detector | `go test -race ./...` | C3 |
| Lint + format | `golangci-lint run ./...` (gofmt/gofumpt) | C2 |
| Static security | `gosec ./...` — findings **fixed** | C4/N8 |
| Vuln audit | `govulncheck ./...` — findings **fixed** | C5/N8 |
| Architecture | `go-arch-lint check` (`.go-arch-lint.yml` authored) | C6 |
| **Mutation testing** | `gremlins unleash ./... --threshold-efficacy=0 --threshold-mcover=0` | **C7/N15/N19 — HARD GATE, fails the build** (no `continue-on-error`) |

**Container image (Hybrid → R1/N17):** `.github/workflows/images.yml` builds & publishes the image. Tags:

- On **tag `X`** (R3): `X`, `X-{ts}`, `X-{commit}`, `X-{commit}-{ts}`.
- On **commit to `master`** (R4): `master-{commit}`, `master-{ts}`, `master-{commit}-{ts}`.

**Default branch (R2/N16):** `master` (never `main`).

---

## 9. TDD (T1)

All features and fixes are developed **test-first**:

- **`@qa`** writes the tests using an **isolated context**.
- **`@developer`** writes the implementation using an **isolated context**.

Tests cover the Forgejo client (against a recorded/fake HTTP server), the tool registry and each tool handler, config loading, and logging redaction — sufficient to hold coverage ≥ 95% and to leave **no gremlins survivors**.

---

## 10. Conformance / self-check

The following MUST / MUST NOT are satisfied by this SPEC and scaffold:

- **M1** official SDK `github.com/modelcontextprotocol/go-sdk`; **M2** transport chosen & justified (§1); **M3** OAuth2 not used & justified (§2).
- **M4** every tool described with title/annotations/instructions (§6); **M5** tools are complete use cases (§6).
- **C1** coverage ≥ 95% gate (fails build); **C2** golangci-lint; **C3** `-race`; **C4** gosec; **C5** govulncheck; **C6** go-arch-lint authored + enforced; **C7** gremlins hard gate (§8).
- **T1** TDD workflow referenced (§9).
- **S1** TLS never in-server; **S2** no secret leakage + redaction; **S3** tools grouped read→write→delete; **S4** no local FS → `ALLOW_DIRS` omitted & explained; **S5** fix-don't-suppress; **S6** local-only module name (§5).
- **A1** DDD/Clean architecture with layout, tool registry, transport wiring, config, error handling (§4).
- **D1** README English; **D2** SPEC/AGENTS strictly English; **D3** README begins with the AI-Generated Content disclaimer; **D4** full badge set.
- **L1–L5** logging channel per transport, `LOG_LEVEL`-gated, `LOG_FILENAME`/`LOG_FORMAT` (§7).
- **R1** image build/publish for Hybrid; **R2** default branch `master`; **R3**/**R4** image tags (§8).
- **G1** Go 1.27 pinned; **G2** Go stated; **G8** logrus.
- **N1, N2, N3 (vacuous), N4, N5, N6, N7, N8, N9, N10, N11, N12, N13, N14, N15, N16, N17, N18, N19, N20** — all avoided/not violated.
