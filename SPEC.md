# mcp-forgejo — SPEC

**Purpose.** `mcp-forgejo` is a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that wraps the **Forgejo REST API** so a model-driven client (editor, CLI, or remote team sidecar) can perform common software-development tasks against an internal Forgejo instance: repositories, files/contents, issues, pull requests, branches, releases, and organizations.

**Language (C02GO).** Go, pinned at the latest stable **1.27.1** (see `go.mod`, `GO_VERSION: "1.27"` in `ci.yml`) — see [C01GO]. The official MCP SDK `github.com/modelcontextprotocol/go-sdk` is used; the protocol is **never hand-rolled** (M01).

**Deployment type (M06).** **HYBRID** — supports both **STDIO** (local editor/CLI companion) and **HTTP/SSE** (remote sidecar). Because it supports HTTP/SSE, it MUST build & publish a container image (R01) and route logs to the channel appropriate per transport (L01). Under DDD/Clean this is achieved with **driver implementations per launch mode** (a `stdio` driver and an `http/sse` driver), selected at startup.

> The implementation of the Go application logic is **not** part of this document's scope. This SPEC defines the contract, architecture, tool surface, security model, logging, and CI/CD so that the scaffold can be built against a fixed specification.

---

## 1. Transport decision (M02 / N08)

The server is **HYBRID: STDIO + HTTP/SSE**. The transport is chosen based on the two distinct ways this server is used:

- **Local editor/CLI companion → STDIO.** The most common MCP integration is a local client (editor, terminal, agent harness) that spawns the server as a child process and speaks MCP over its standard input/output. This needs **no listener and no open ports**, is short-lived per session, and is the simplest, most secure local deployment. Over stdio, the **stdout channel is reserved for the MCP protocol**, so logs go to a file (L01).
- **Remote/team sidecar → HTTP/SSE.** For a long-running, always-on deployment serving multiple concurrent sessions (a shared team gateway, a container, a Kubernetes sidecar behind a reverse proxy), a stateless HTTP endpoint with Server-Sent Events is required. This supports remote clients, concurrency, and standard deployment tooling.

Both transports are driven by the **same tool registry and application layer**; only the transport driver and the logging sink differ. This gives a single binary that is useful both locally and remotely.

---

## 2. Auth decision (M03 / N08) — OAuth2 is NOT used

**OAuth2 is not used.** The server authenticates to Forgejo with a **personal access token (PAT)**, sent as an `Authorization: token <PAT>` header on every Forgejo API call. The PAT is sourced per-transport (M06): over **HTTP** it is accepted per-request from the client's `Authorization: Bearer <token>` HTTP header (remote auth via headers); over **stdio** it is supplied via the `FORGEJO_TOKEN` environment variable (local auth via env).

**Why no OAuth2:** the target is an **internal** Forgejo instance, and the Forgejo swagger exposes **no OAuth2 client flow** for the operations this server performs — there is no authorization-code or client-credentials endpoint usable here. A single PAT is the natural, minimal mechanism: it is scoped to the calling user, long-lived, trivially issued/revoked in the Forgejo UI, and sufficient for both launch modes:

- **stdio (local):** the PAT is supplied to the server by the local operator via `FORGEJO_TOKEN` in the environment — the standard local-credential path. `FORGEJO_TOKEN` is **required** for the stdio transport.
- **HTTP/SSE (remote):** the MCP client authenticates each HTTP request with an `Authorization: Bearer <token>` header. The server reads that header on every incoming request, injects the token into the request context, and uses it (as `Authorization: token <PAT>`) on the outbound Forgejo call, overriding any config fallback. `FORGEJO_TOKEN` is **optional** for the HTTP transport (it is not required at startup; it only serves as a fallback when no per-request token is present). **Requests lacking a valid Bearer token are rejected with `401 Unauthorized`.**

OAuth2 would be warranted only if the server had to **delegate authorization to untrusted remote clients** (token exchange with the MCP SDK). That is not the case: `mcp-forgejo` is a server-side client of Forgejo, not an identity provider, and it serves a closed, trusted team. Hence **PAT, no OAuth2** (S02 data-hygiene rules apply to the token; see §5).

---

## 3. Configuration (A01)

Configuration is loaded from the environment with [`kelseyhightower/envconfig`](https://github.com/kelseyhightower/envconfig) into a single `Config` struct (`config`).

| Env var          | Type       | Default            | Description |
|------------------|------------|--------------------|-------------|
| `FORGEJO_URL`    | `string`   | (from env)         | Base URL of the Forgejo instance, e.g. `https://git.example.com`. Required. |
| `FORGEJO_TOKEN`  | `string`   | (empty)            | Forgejo **personal access token** (PAT). **Secret** — never logged, echoed, or included in tool output (S02/N02). **Required for the stdio transport; optional for the HTTP transport** (over HTTP the token is supplied per-request via `Authorization: Bearer <token>`; `FORGEJO_TOKEN` is only a fallback). |
| `LISTEN_ADDR`    | `string`   | `:8080`            | **MCP listen address** for the HTTP/SSE MCP transport (O04). The internal observability listener is a separate address, `INTERNAL_ADDR`. |
| `INTERNAL_ADDR`  | `string`   | `:8081`            | **Internal observability address** — a separate listener for `/metrics`, `/debug/pprof/*`, `/healthz`, `/readyz`, distinct from the MCP listen address (O01/O04/N32). Only started in HTTP/SSE mode (see §7.1). |
| `LOG_LEVEL`      | `string`   | (unset)            | Logging level (`trace`, `debug`, `info`, `warn`, `error`). **Mode-dependent (L02):** HTTP/SSE mode always logs, defaulting to `info` when unset; stdio mode logs **only when set** — unset ⇒ disabled. |
| `LOG_FILENAME`   | `string`   | `/tmp/mcp-forgejo.log` | Log file path for the **stdio** transport (chmod **600**). Ignored for HTTP/SSE (L01, L03). |
| `LOG_FORMAT`     | `string`   | `text`             | `text` (logrus text, full absolute timestamp) or `json` (L04). |

**`ALLOW_DIRS` is intentionally absent.** The server **never touches the local filesystem** — every operation is an HTTP call to the remote Forgejo API (S04 applies only when the server touches the local filesystem; here it does not). Files are read/written **on the Forgejo server**, not on the machine running `mcp-forgejo`. Therefore no directory scoping is required, and `ALLOW_DIRS` is omitted from config and CLI. This is stated explicitly here so reviewers do not expect it.

---

## 4. Architecture (A01 / DDD / Clean)

The application is layered with Clean / DDD principles. The **composition root** wires concrete dependencies; the core depends on interfaces.

> **Layout decision (A01):** this repository **intentionally uses a layered
> top-level package layout** — `cmd/mcp-forgejo` (composition root) plus
> `{domain,application,infrastructure,server,config,logging}` — **by
> explicit project choice**. This is a deliberate decision recorded here (in
> contrast to the skill's default **simple layout**, which is used only when no
> layered top-level layout was requested). It is enforced by `go-arch-lint`
> (§4.5) and reflected in the package tree below; do not flatten it into a
> single/`main`-only package without revisiting this decision.

### 4.1 Package layout

```
cmd/mcp-forgejo/
  main.go                # composition root: load config, build logging, construct
                         # Forgejo client + tool registry, select transport driver, serve.
domain/                # pure entities + interfaces (Repo, File, Issue, PullRequest,
                         # Release, Organization, Comment, Branch, Commit). No deps.
application/           # use cases / tool handlers implementing domain interfaces.
infrastructure/
  forgejo/             # Forgejo REST client (resty.dev/v3) implementing the domain
                         # interfaces; auth header, pagination, error mapping.
server/                # MCP tool registry + transport drivers (stdio, http/sse).
config/                # envconfig Config struct + load/validate.
logging/               # logrus setup, sink per transport, redaction helpers.
```

### 4.2 Tool registry

The **tool registry** (`server`) is a single map of tool name → `Tool` where each `Tool` carries:

- the **metadata** (title, annotations, per-tool instructions, input/output JSON Schema — see §6),
- a **handler** function that invokes the corresponding **application use case**,
- its **priority group** (read / write / delete) used to order registration (S03).

At startup the registry is populated once, then bound to the MCP SDK `Server` (via `server.RegisterTool(...)` from the official go-sdk). The same registry is used by both transport drivers, guaranteeing identical tool surfaces over stdio and HTTP/SSE.

### 4.3 Transport wiring

Transport selection happens at startup, in the composition root:

- **stdio driver** (`server/stdio.go`) — connects the SDK server to `os.Stdin`/`os.Stdout`. Logging sink = **file** (`LOG_FILENAME`, chmod 600).
- **http/sse driver** (`server/http.go`) — serves the MCP HTTP/SSE endpoint on `LISTEN_ADDR`. Logging sink = **stdout** (12-factor).

A single launch-mode flag selects the driver (`-mode stdio|http`, defaulting to `stdio` per M06). Both drivers share the same `Server` construction, tools, and application layer.

### 4.4 Error handling (taxonomy → MCP error codes)

Forgejo responses are mapped to a small error taxonomy in `infrastructure/forgejo`, then to MCP error codes:

| Taxonomy            | Trigger (Forgejo HTTP)                       | MCP error code / handling |
|---------------------|----------------------------------------------|---------------------------|
| `validation`        | 422 Unprocessable Entity, 409 Conflict       | `INVALID_PARAMETERS` — surface a friendly message; no retry. |
| `notFound`          | 404 Not Found                                | `NOT_FOUND` / tool returns a clear "not found" result. |
| `forbidden`         | 403 Forbidden, 423 repoArchived              | `ACCESS_DENIED` — scoped; never leaks token details. |
| `unauthorized`      | 401 Unauthorized                             | `ACCESS_DENIED` — token missing/revoked; log a generic message (never the token). |
| `transient`         | 5xx, timeouts, network                       | `INTERNAL_ERROR` / `RESOURCE_BUSY` — safe to retry with backoff. |

Errors are **logged** at an appropriate level (transient at warn, others at info/debug) and surfaced in the tool result with **no** sensitive data (S02).

### 4.5 Dependency boundaries

`go-arch-lint` enforces the edges in `.go-arch-lint.yml`:

- `cmd` → any top-level package (composition root).
- `domain` → nothing (pure core).
- `application` → `domain` only.
- `infrastructure` → `domain` only.
- `server` → `domain`, `application`, `infrastructure` (the adapter that binds layers).
- `config`, `logging` → nothing internal.

Layers communicate through **domain interfaces**; `application` and `infrastructure` never import each other directly.

### 4.6 State model (X01)

`mcp-forgejo` is a **stateless** server (X01):

- Every tool call is a **self-contained HTTP request** to the remote Forgejo instance; no request depends on any state produced by a previous one.
- All mutable state — repositories, issues, PRs, releases, tags, milestones, labels — is **held by Forgejo** and read/written over its REST API. The server holds no mirror of it.
- The only process-scoped values are **immutable configuration** (`FORGEJO_URL`, `FORGEJO_TOKEN`, logging/transport settings) loaded once at startup and never mutated at runtime.
- There is **no local storage, database, or filesystem state** (S04), hence **no migration step, no schema, and no persistence layer** to reason about.
- Consequently the **MCP session/request lifecycle semantics (X04/X05) are not applicable**: there is no session-scoped or server-scoped mutable state to share, snapshot, or reset. Correlation IDs (`request_id`) and client-IP (`source`) are derived per-request for logging only (L09/L04GO) and carry no state.

Because the server is stateless, scaling (e.g. multiple HTTP/SSE replicas behind a load balancer) is trivial: any replica can serve any request with no shared state.

---

## 5. Security (S01–S06)

- **S01 / N01 — TLS never in-server.** TLS is **NEVER implemented inside the server** for the HTTP/SSE transport. The HTTP/SSE listener serves plain HTTP; TLS termination is always the **reverse proxy's** job (nginx / Caddy / ingress) in front of the container. No in-server TLS code, certificates, or key handling exists or is planned.
- **S02 / N02 — data hygiene / redaction.** The `FORGEJO_TOKEN` (and any credentials/passwords) are **never** echoed in logs, tool outputs, error messages, or debug dumps. The PAT is read into config once and used only as the outbound `Authorization: token <PAT>` header. Tool output contracts contain no token field; any server string that could embed credentials is redacted by a shared helper before being returned or logged.
  - **Redaction is centralized in helpers, not per-field tags (S02).** Because no API response or request struct carries a token field (the PAT is only ever an outbound header, never deserialized into a struct), redaction is **not** implemented via per-field `secret:"true"`-style struct tags. Instead it is centralized in two shared helpers that every logging/error path funnels through: `domain.Redact(s, secret)` (scrubs a string against the PAT before it is surfaced in an error or log) and `redactArgs(raw)` (in `server/session.go`, which replaces sensitive tool-call argument keys — `token`, `password`, `passwd`, `secret`, `apikey`, `access_key`, `private_key`, `authorization`, `cookie`, `pat` — with `[REDACTED]` before logging, matched case-insensitively). The single `FORGEJO_TOKEN` config field therefore does not need a redaction tag (and envconfig ignores struct tags anyway); the helpers are the single source of truth for data hygiene.
- **S09 / N23 — control-character sanitization of free text.** Tool results that return **free-form text** captured from Forgejo (`forgejo_diff_get`/`forgejo_pull_diff` → `Diff.Text`, and `forgejo_file_get` → `File.Content`) are passed through `stripControl` (in `infrastructure/forgejo/forgejo.go`) before being returned. `stripControl` removes ANSI escape sequences and C0 control characters (preserving the structural whitespace `\n`, `\t`, `\r`) so that a client rendering the text verbatim cannot be driven by terminal-control injection embedded in upstream content (S09/N23).
- **S03 — tool priority order.** Tools are grouped and registered **read → write/update → delete** (see §6).
- **S04 — no local filesystem access.** The server does not touch the local filesystem; it only talks to the remote Forgejo API over HTTP. Consequently **`ALLOW_DIRS` is not required and is omitted** from config (see §3). There is no local path to scope, so N03 is vacuous by design.
- **S05 / N02GO — fix, don't suppress.** gosec and govulncheck findings are **fixed**, never suppressed via blanket exclusions or default `#nosec`. Findings block the build (C05GO/C06GO). Where a finding **cannot** be fixed — e.g. a **test-only** dependency that is not linked into the production binary and has no upstream fix — it is excluded by **scoping the scan to production packages** (`govulncheck ./cmd/... ./domain/... ./application/... ./infrastructure/... ./server/... ./config/... ./logging/... ./observability/...`) rather than suppressing the finding, and the exclusion is documented here with its justification. Current justified exclusion: the e2e suite depends on `github.com/docker/docker` (pulled transitively via `go-docker-testsuite`) to run a real Forgejo container; it is **not** part of the release binary (`go list -deps ./cmd/mcp-forgejo` contains no `docker/*`), and the two findings (GO-2026-4887, GO-2026-4883) have **Fixed in: N/A**.
- **S06 / N16 (not violated) — module path matches the public location.** The module/package name `github.com/teran/mcp-forgejo` matches the canonical public repository location and must be kept in sync with it. It is not a placeholder. Because this is a **public GitHub.com** repository, the standard public module path is correct (S06 permits standard public paths for public GitHub servers); the internal-Forgejo restriction behind N16 does not apply.
- **S07 — reverse-proxy authn/authz for HTTP/SSE.** The MCP HTTP/SSE layer offers **no authentication of its own**: the server holds the PAT in its environment and uses it to call Forgejo, but any client that can reach `LISTEN_ADDR` can drive every tool with the operator's privileges — including destructive ones (`forgejo_repo_delete`, `forgejo_pull_merge`, …). For any HTTP/SSE deployment the listener **MUST** sit behind a reverse proxy that enforces client authentication/authorization (e.g. mTLS, OIDC, network ACL, or a proxy token) before requests reach the server. Additionally, run the PAT under a **dedicated low-privilege Forgejo user** scoped to only the operations the team actually needs. In **stdio** mode the client is trusted by construction (the local process that spawned the server), so this does not apply.

---

## 6. Tool surface (M04, M05, S03)

All tools target the **closed domain** of the configured Forgejo instance (`openWorldHint: false`). Every tool is a **complete use case** (M05): a client accomplishes a finished task in a single call without chaining low-level primitives. Each tool maps to one or more Forgejo `operationId`s from the Forgejo swagger.

**Common request parameters:** `owner`, `repo`, `ref`/`branch`, pagination `page` (1-based) + `limit`. **Common errors:** 401, 403, 404, 422, 409, 423. Content in file operations is base64-encoded over the wire; bodies carry commit metadata (message, branch, `new_branch`, author).

> **Status.** §6 documents the **implemented** tool surface: **all 51 tools** below
> are registered in the running server via `mcp.AddTool(...)` in `server/server.go`
> (the single tool registry, §4.2) and are grouped **read → write/update → delete**
> (S03). There is no separate "target" vs "implemented" split — what is listed here
> is what the server actually exposes, over both stdio and HTTP/SSE. The tool count
> is verified from the registry: 19 read + 22 write/update + 10 delete = **51**.

### 6.0 Metadata conventions

- **read** tools → `readOnlyHint: true`, `destructiveHint: false`, `idempotentHint: true`, `openWorldHint: false`.
- **write/update** tools → `readOnlyHint: false`, `destructiveHint: false`, `openWorldHint: false`; `idempotentHint: true` only where repeating with identical args has no extra effect.
- **delete** tools → `readOnlyHint: false`, `destructiveHint: true`, `idempotentHint: false`, `openWorldHint: false` (deleting an already-removed resource typically errors, so delete is **not** idempotent).

Representative JSON-Schema-style metadata block (shown for one tool; all tools carry the same five annotations; per-tool model guidance is carried in the tool `description` — the go-sdk v1.8.0 `Tool` has no separate `instructions` field, so model instructions are merged into `description`):

```json
{
  "name": "forgejo_file_get",
  "title": "Read file",
  "description": "Returns the UTF-8 text content of a file at path+ref, plus commit/sha metadata. Use to read a single file's content from the configured Forgejo repo. Provide the file path and an optional ref (branch/tag/sha); defaults to the default branch. Returns base64-decoded UTF-8 text plus the blob sha and commit metadata. If the blob is not valid UTF-8 (a binary file such as an image or archive), the tool does NOT return corrupted text — it returns a binary flag instead of decoding. Never attempts to read local filesystem paths.",
  "annotations": {
    "title": "Read file",
    "readOnlyHint": true,
    "destructiveHint": false,
    "idempotentHint": true,
    "openWorldHint": false
  },
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

### 6.1 READ — `readOnlyHint: true`, `openWorldHint: false` (19 tools)

| # | Tool | Title | Forgejo operationId(s) | Instructions (abridged) |
|---|------|-------|------------------------|--------------------------|
| 1 | `forgejo_repo_search` | Search repositories | `repoSearch` | Search the Forgejo instance by `q`, `topic`, `sort`, `order`, `private`. Returns matching repos. `idempotentHint: true`. |
| 2 | `forgejo_repo_get` | Get repository | `repoGet`, `repoGetByID` | Return details of one repo by owner+name (or id). Read-only; never modifies. |
| 3 | `forgejo_repo_list_contents` | List directory contents | `repoGetContentsList` | List entries (type/sha/size) in a directory at `path`+`ref`. Read-only. |
| 4 | `forgejo_file_get` | Read file | `repoGetContents`, `repoGetRawFile` | Return base64-decoded UTF-8 content of a file at `path`+`ref`, plus commit/sha metadata (see block above). Binary blobs return a `binary` flag instead of decoding. Read-only. |
| 5 | `forgejo_org_list` | List my organizations | `orgListCurrentUserOrgs` | List the organizations the current user belongs to. Read-only. |
| 6 | `forgejo_issue_get` | Get issue + comments | `issueGetIssue`, `issueGetComments` | Single call returning an issue **and** its comments. Read-only. |
| 7 | `forgejo_issue_list` | List issues | `issueListIssues` | List issues with a `state` filter (`open`/`closed`/`all`) + pagination. Read-only. |
| 8 | `forgejo_diff_get` | Get diff | `repoCompareDiff`, `repoDownloadPullDiffOrPatch` | Return a unified diff between two refs (`basehead`) or for a PR, as text. Read-only. |
| 9 | `forgejo_commit_list` | List commits | `repoGetAllCommits` | List commits of a repo branch with pagination. Read-only. |
| 10 | `forgejo_branch_list` | List branches | `repoListBranches` | List branches of a repo. Read-only. |
| 11 | `forgejo_branch_get` | Get branch | `repoGetBranch` | Return a single branch of a repository by name. Read-only. |
| 12 | `forgejo_pull_list` | List pull requests | `repoListPullRequests` | List PRs with a `state` filter + pagination. Read-only. |
| 13 | `forgejo_pull_get` | Get pull request + files + checks | `repoGetPullRequest`, `repoGetPullRequestFiles`, `repoGetCombinedStatusByRef` | Single call returning a PR **with** its changed files **and** combined checks status. Read-only. |
| 14 | `forgejo_release_list` | List releases | `repoListReleases`, `repoGetLatestRelease` | List releases of a repo, or fetch the latest. Read-only. |
| 15 | `forgejo_tag_list` | List tags | `repoListTags` | List the git tags of a repository. Read-only. |
| 16 | `forgejo_milestone_list` | List milestones | `issueGetMilestonesList` | List the milestones of a repository. Read-only. |
| 17 | `forgejo_label_list` | List labels | `issueListLabels` | List the labels of a repository. Read-only. |
| 18 | `forgejo_user_get` | Get user | `userGet`, `userGetCurrent` | Return a user by username; when `username` is omitted, return the current authenticated user. Read-only. |
| 19 | `forgejo_user_list` | Search users | `userSearch` | Search Forgejo users by a query string. Read-only. |

### 6.2 WRITE / UPDATE — `readOnlyHint: false`, `destructiveHint: false` (22 tools)

`idempotentHint: true` is set only where repeating with identical args has no extra effect; `false` where every call records a new resource/commit.

| # | Tool | Title | Forgejo operationId(s) | `idempotent` | Instructions (abridged) |
|---|------|-------|------------------------|--------------|--------------------------|
| 20 | `forgejo_repo_create` | Create repository | `createCurrentUserRepo`, `orgCreateRepo` | false | Create a repo (`name`, optional `owner`/org, `private`, `auto_init`, plus template fields). Creating a name that already exists **conflicts** — not idempotent. |
| 21 | `forgejo_repo_update` | Update repository | `repoEdit` | true | Edit a repo's description, website, default branch or visibility. Repeating the same edit is idempotent. |
| 22 | `forgejo_repo_fork` | Fork repository | `repoCreateFork` | false | Fork a repository into an organization or user namespace. Not idempotent. |
| 23 | `forgejo_org_create` | Create organization | `orgCreate` | false | Create an organization (`username`, optional `description`, `full_name`). Creating a name that already exists **conflicts** — not idempotent. |
| 24 | `forgejo_file_write` | Write/update file | `repoCreateFile`, `repoUpdateFile` | false | **Single call covering create AND update**: write `content` (text, base64-encoded on the wire) at `path`+`branch` with a commit `message`. Creates if absent, updates if present. Not idempotent: every call records a new commit. |
| 25 | `forgejo_file_write_many` | Write multiple files | `repoChangeFiles` | false | Modify several files in **one commit** (multi-file single commit) at a branch with a commit message. |
| 26 | `forgejo_branch_create` | Create branch | `repoCreateBranch` | false | Create a branch from an existing ref. Creating an existing branch **conflicts**. |
| 27 | `forgejo_issue_create` | Create issue | `issueCreateIssue` | false | Create an issue with `title`, `body`, `labels`, `milestone`. |
| 28 | `forgejo_issue_update` | Update/close/reopen issue | `issueEditIssue` | true | **Single tool handles edit + close + reopen**: update fields; set `state` to `open`/`closed`. Repeating the same edit is idempotent. |
| 29 | `forgejo_issue_comment_add` | Add issue comment | `issueCreateComment` | false | Append a comment to an issue. Each call adds a new comment. |
| 30 | `forgejo_issue_set_labels` | Set issue labels | `issueReplaceLabels` | true | Replace the exact set of labels on an issue by their IDs. Repeating the same set is idempotent. |
| 31 | `forgejo_pull_create` | Create pull request | `repoCreatePullRequest` | false | Open a PR from `head`→`base` with `title`/`body`. |
| 32 | `forgejo_pull_update` | Update/close/reopen PR | `repoEditPullRequest` | true | **Single tool handles edit + close + reopen**: update fields; set `state`. |
| 33 | `forgejo_pull_merge` | Merge pull request | `repoMergePullRequest`, `repoPullRequestIsMerged` | false | Merge a PR with a `merge`/`squash`/`rebase` method and report the result (merged vs already-merged). |
| 34 | `forgejo_pull_review` | Review pull request | `repoCreatePullReview`, `repoSubmitPullReview` | false | **Create AND submit** a PR review (`approve`/`comment`/`request_changes`) in one call. |
| 35 | `forgejo_release_create` | Create release | `repoCreateRelease` | false | Create a release for an existing `tag` with `title`/`notes`. |
| 36 | `forgejo_release_asset_upload` | Upload release asset | `repoCreateReleaseAttachment` | false | Upload content (base64) as a named file attachment to a release. Not idempotent: each call creates a new asset. |
| 37 | `forgejo_tag_create` | Create tag | `repoCreateTag` | false | Create a git tag pointing at a ref with an optional message. Creating an existing tag **conflicts**. |
| 38 | `forgejo_milestone_create` | Create milestone | `issueCreateMilestone` | false | Create a milestone with a title, description and due date. Creating an existing title **conflicts**. |
| 39 | `forgejo_milestone_update` | Update milestone | `issueEditMilestone` | true | Edit a milestone's title, description, state or due date. Repeating the same edit is idempotent. |
| 40 | `forgejo_label_create` | Create label | `issueCreateLabel` | false | Create a label with a name, color and description. Creating an existing name **conflicts**. |
| 41 | `forgejo_label_update` | Update label | `issueEditLabel` | true | Edit a label's name, color or description. Repeating the same edit is idempotent. |

### 6.3 DELETE — `readOnlyHint: false`, `destructiveHint: true` (10 tools)

All delete tools are **destructive** (S12 / HITL) and **not idempotent** (deleting an already-removed resource typically errors).

| # | Tool | Title | Forgejo operationId(s) | Instructions (abridged) |
|---|------|-------|------------------------|--------------------------|
| 42 | `forgejo_file_delete` | Delete file | `repoDeleteFile` | Delete a file at `path`+`branch` with a commit `message`. **Destructive** — confirm before use. |
| 43 | `forgejo_branch_delete` | Delete branch | `repoDeleteBranch` | Delete a branch. **Destructive** — confirm before use; will not delete the default branch. |
| 44 | `forgejo_issue_delete` | Delete issue | `issueDelete` | Permanently delete an issue. **Destructive** — confirm before use. |
| 45 | `forgejo_comment_delete` | Delete comment | `issueDeleteComment` | Delete an issue/PR comment. **Destructive** — confirm before use. |
| 46 | `forgejo_release_delete` | Delete release | `repoDeleteRelease` | Delete a release (tag remains). **Destructive** — confirm before use. |
| 47 | `forgejo_repo_delete` | Delete repository | `repoDelete` | **Permanently delete a repository.** Highly destructive — instructions require explicit user confirmation before invoking. |
| 48 | `forgejo_org_delete` | Delete organization | `orgDelete` | **Permanently delete an organization.** Highly destructive — confirm before use. |
| 49 | `forgejo_tag_delete` | Delete tag | `repoDeleteTag` | Delete a git tag by its name. **Destructive** — confirm before use. |
| 50 | `forgejo_milestone_delete` | Delete milestone | `issueDeleteMilestone` | Delete a milestone by its ID. **Destructive** — confirm before use. |
| 51 | `forgejo_label_delete` | Delete label | `issueDeleteLabel` | Delete a label by its ID. **Destructive** — confirm before use. |

> **Data-hygiene (S02):** none of the above tools accept or return a token/credential. Auth is injected server-side (per-request Bearer token over HTTP, `FORGEJO_TOKEN` over stdio, M06); outputs never contain it.

---

## 7. Logging & Observability (L01–L06, B05, L01GO, O01–O04, N32)

Uses **`logrus`** (`github.com/sirupsen/logrus`).

- **L01 — channel per transport:** HTTP/SSE → **stdout** (12-factor); stdio → **file** (default `/tmp/mcp-forgejo.log`, **chmod 600**), **never** stdout (stdout is the MCP protocol channel).
- **L02 — mode-dependent enablement (M06):** in **HTTP/SSE** mode (`-mode http`) logging is **always enabled**, default level **`info`** (overridable via `LOG_LEVEL`). In **stdio** mode (default) logging is **enabled only when `LOG_LEVEL` is set** — unset ⇒ no logs.
- **L03 — override path:** `LOG_FILENAME` (default `/tmp/mcp-forgejo.log`).
- **L04 — format:** default `text` (logrus text, **full absolute timestamp**); `LOG_FORMAT=json` → JSON.
- **L05 / S02 / N02 — no secrets:** `FORGEJO_TOKEN` and any credentials/passwords are **never** logged; redaction helpers strip them from any log line or error before emission.
- **L06 / B05 — startup banner:** when logging is enabled (always in HTTP/SSE mode — default `info`; in stdio only when `LOG_LEVEL` is set, L02), the **startup banner** is emitted as the **first line** of the logging channel for the selected transport — the **file** for stdio, **stdout** for HTTP/SSE (consistent with L01). The banner advertises the running build and is written exactly once, at startup, before any other log line.
- **B05 — banner format:** the banner text is
  `Starting {appName}/{appVersion} (commit: {appCommitHash}; built at {appTimestamp})`.
  The fields are the build metadata embedded at link time via ldflags (B02). The banner contains **no secrets** (S02).

### 7.1 Metrics & Observability (O01–O04, N32)

Because `mcp-forgejo` is **HYBRID** (§1), it is subject to the **Remote** observability
requirements whenever it is launched in **HTTP/SSE** mode (O01/N32): it exposes an
**internal observability endpoint** on a **separate listener**, distinct from the MCP
listen address, so a reverse proxy forwards **only** the MCP transport (`LISTEN_ADDR`)
and never the observability one. Observability is **always enabled** for HTTP/SSE —
there is **no opt-out flag** (N32). It is **not** started in stdio mode (stdio owns
stdout and has no HTTP listener).

- **Listen vs internal address (O04):** the MCP listen address is **`LISTEN_ADDR`**
  (default **`:8080`**), configured via `config.ListenAddr`; the internal observability
  address is **`INTERNAL_ADDR`** (default **`:8081`**), configured via
  `config.InternalAddr`. Both are env-overridable and served on distinct listeners so a
  reverse proxy forwards only the MCP transport to `:8080`.
- **Endpoints (served by `observability.NewHandler()`, `observability/observability.go`):**
  - `GET /metrics` — Prometheus metrics via `promhttp.Handler()` on the **default
    registry**, which already includes the **standard Go collectors** — Go
    runtime/memstats (`go_goroutines`, `go_gc_duration_seconds`, `go_memstats_*`,
    …) **and net/http** (`promhttp_metric_handler_requests_total`, …) (O02).
  - `GET /debug/pprof/` + `/debug/pprof/cmdline`, `/profile`, `/symbol`, `/trace` —
    standard `net/http/pprof` profiling handlers.
  - `GET /healthz` — liveness probe (always `200`).
  - `GET /readyz` — readiness probe (always `200`).
  - These are served on their **own `http.Server`**, entirely separate from the MCP
    JSON-RPC/SSE flow (O01): metrics and probes are **never** part of the MCP tool surface.
- **Upstream metrics (O03):** `mcp-forgejo` exposes upstream Forgejo response metrics
  on `/metrics` (alongside the standard Go collectors, O02). The `UpstreamCollector`
  (`observability/upstream.go`) is registered on the default registry and observed
  once per outbound Forgejo request by the infrastructure client (`Client.observer`,
  O03), exposing the four metrics:
  - `forgejo_upstream_request_duration_seconds` — histogram of request duration in seconds;
  - `forgejo_upstream_request_total` — counter labeled `{method,status}` (status = numeric
    HTTP status as string; `0` on transport error);
  - `forgejo_upstream_request_size_bytes` — histogram of request body size;
  - `forgejo_upstream_response_size_bytes` — histogram of response body size.
  The observer is wired from the composition root (`cmd/mcp-forgejo/main.go`) via
  `server.BuildWithObserver`.
- **Wiring (O04/N32):** the observability listener is started in `cmd/mcp-forgejo/main.go`
  only in the `http` mode branch, alongside the MCP HTTP server, and shut down
  gracefully on context cancellation (graceful 5 s shutdown in `observability.Run`). In
  stdio mode it is never started.

---

## 8. CI & tooling (C01, C02, C03GO, C04GO, C05GO, C06GO, C07GO, C08GO, C01GO, R01–R04, B01–B05)

The Go profile is enforced in CI (`.github/workflows/ci.yml`) and locally:

| Check | Command | Gate |
|-------|---------|------|
| Go version | `go 1.27.1` in `go.mod` == `GO_VERSION: "1.27"` | C01GO/N01GO |
| Coverage | `go test -race -coverprofile=cover.out -covermode=atomic ./...` + total **≥ 95%** | C01/N06 (build fails below 95) |
| Race detector | `go test -race ./...` | C04GO |
| Lint + format | `golangci-lint run ./...` (gofmt/gofumpt) | C03GO |
| Static security | `gosec ./...` — findings **fixed** | C05GO/N02GO |
| Vuln audit | `govulncheck ./cmd/... ./domain/... ./application/... ./infrastructure/... ./server/... ./config/... ./logging/... ./observability/...` — prod findings **fixed**; test-only deps with no fix excluded (see S05) | C06GO/N02GO |
| Secret scan (git history) | `gitleaks detect --source . --redact --verbose` over **full history** (`actions/checkout@v4` + `fetch-depth: 0`) — findings **fixed**, never suppressed; no `continue-on-error` | N28/C03 |
| Architecture | `go-arch-lint check` (`.go-arch-lint.yml` authored) | C07GO |
| **Mutation testing** | `gremlins unleash . --threshold-efficacy=90 --threshold-mcover=80 --timeout-coefficient=60` | **C08GO/N03GO/N15 — HARD GATE, fails the build** (no `continue-on-error`; run from the module root `.`, not `./...`) |
| **End-to-end tests** | `make e2e` ⇒ `go test -tags e2e ./...` (go-docker-testsuite harness) | **T02/C04/N30 — HARD GATE, fails the build** (dedicated CI job `e2e`, no `continue-on-error`) |

> **e2e (T02, C04, C09GO, N30).** The e2e suite verifies the **full path between the MCP
> tool handler and a real Forgejo backend**. It runs through the
> **`github.com/teran/go-docker-testsuite`** harness (pulling `…/applications/forgejo
> v1.6.0` to spin up a real Forgejo container) and is **build-tagged**
> (`//go:build e2e` on every file in `e2e/`), so it is **excluded from the default unit
> run** (`make test` / `go test ./...`). It is exposed via the **`make e2e`** target
> (`go test -tags e2e ./...`) and runs in a **dedicated CI job** (`e2e` in
> `.github/workflows/ci.yml`) that is a **hard gate** — a failing e2e test breaks CI
> (C04; no `continue-on-error`). `make e2e` requires a running Docker daemon.

> **Gremlins `--threshold-mcover` = 80 (goal reached).** The 80 goal is now met
> by raising `--timeout-coefficient` to 60 per the previously-documented plan.
> gremlins 0.6.x derives the per-mutant timeout from the baseline suite time
> (default coefficient 3), so on a warm Go test cache (the CI-equivalent run,
> `actions/setup-go` `cache: true`) the short timeout made most mutants report
> "Timed out" and excluded them from the coverage/efficacy accounting — capping
> reported mutant coverage near **21–35%**. With `--timeout-coefficient=60`
> every mutant gets a long-enough window: a warm-cache run exercises all 244
> mutants (0 timed out) and reports **mcover ≈ 83%** with **efficacy ≈ 97%**
> (≥ 90). The ~6 remaining live mutants are near-equivalent (time-constant
> arithmetic and `len(query) > 0` / `err != nil` boundary cases) that would need
> production refactoring to kill, so efficacy is left at ~97%. Note gremlins
> 0.6.x's warm-cache coverage accounting is fragile (it can report lines as
> NOT COVERED that `go tool cover` shows at 100%), so the exact reported mcover
> varies with the run; 80 is set with the ~83% warm-cache measurement in mind.

**Container image (Hybrid → R01/N14):** `.github/workflows/images.yml` builds & publishes the image. Tags:

- On **tag `X`** (R03): `X`, `X-{ts}`, `X-{commit}`, `X-{commit}-{ts}`.
- On **commit to `master`** (R04): `master-{commit}`, `master-{ts}`, `master-{commit}-{ts}`.

**Default branch (R02/N13):** `master` (never `main`).

**Binary release (B01/B02):** `.github/workflows/release.yml` runs `goreleaser release --clean` on every git **tag `v*`** (requires `contents: write`), publishing the **binary artifact** to a GitHub Release. GoReleaser embeds **build metadata via ldflags** (B02) with the variable names `appName`, `appVersion`, `appCommitHash`, `appTimestamp` (`main.*` package vars):

- `appName` ← `{{ .ProjectName }}`
- `appVersion` ← `{{ .Version }}`
- `appCommitHash` ← `{{ .ShortCommit }}`
- `appTimestamp` ← `{{ .Date }}`

**Single-build-source (B04):** the binary published by `release.yml` (and built by `images.yml`) is the **only** build of the server. The `Dockerfile` does **NOT** recompile — it only copies a GoReleaser-produced binary (`dist/mcp-forgejo`) into the image. The startup banner (B05/L06) reads this embedded metadata.

---

## 9. TDD (T01)

All features and fixes are developed **test-first**:

- **`@qa`** writes the tests using an **isolated context**.
- **`@developer`** writes the implementation using an **isolated context**.

Tests cover the Forgejo client (against a recorded/fake HTTP server), the tool registry and each tool handler, config loading, and logging redaction — sufficient to hold coverage ≥ 95% and to leave **no gremlins survivors**.

---

## 10. Conformance / self-check

The following MUST / MUST NOT are satisfied by this SPEC and scaffold:

- **M01** official SDK `github.com/modelcontextprotocol/go-sdk`; **M02** transport chosen & justified (§1); **M03** OAuth2 not used & justified (§2).
- **M04** every tool described with title/annotations/instructions (§6); **M05** tools are complete use cases (§6).
- **C01** coverage ≥ 95% gate (fails build); **C03GO** golangci-lint; **C04GO** `-race`; **C05GO** gosec; **C06GO** govulncheck; **C07GO** go-arch-lint authored + enforced; **C08GO** gremlins hard gate (§8).
- **T01** TDD workflow referenced (§9); **T02/C04/N30** e2e build-tagged, run via `make e2e` in a dedicated CI hard-gate job (§8).
- **O01** observability endpoint on a separate `:8081` (`INTERNAL_ADDR`) always present in HTTP/SSE mode, no opt-out; **O02** standard Go runtime/net-http collectors on `/metrics`; **O03** upstream metrics claimed (four `forgejo_upstream_*` metrics on `/metrics`, observed per outbound request, §7.1); **O04** `LISTEN_ADDR` (MCP listen) vs `INTERNAL_ADDR` (observability) — both env-overridable (**N32** satisfied, §7.1).
- **S01** TLS never in-server; **S02** no secret leakage + redaction (centralized helpers, not per-field tags — S02); **S03** tools grouped read→write→delete; **S04** no local FS → `ALLOW_DIRS` omitted & explained; **S05** fix-don't-suppress; **S06** module path matches the public location (§5); **S09** control-character sanitization of free text (S09/N23, §5).
- **A01** DDD/Clean architecture with layout, tool registry, transport wiring, config, error handling (§4); **X01** stateless state model with no local storage/migrations (§4.6).
- **D01** README English; **D02** SPEC/AGENTS strictly English; **D03** README begins with the AI-Generated Content disclaimer; **D04** full badge set.
- **L01–L05** logging channel per transport, mode-dependent enablement (HTTP always on at `info`; stdio `LOG_LEVEL`-gated), `LOG_FILENAME`/`LOG_FORMAT` (§7); **L06** startup banner first line per transport.
- **B01** binary release on `v*` tags via GoReleaser; **B02** ldflags-embedded build metadata (`appName`/`appVersion`/`appCommitHash`/`appTimestamp`); **B04** image reuses the binary, never recompiles; **B05** banner format (§7, §8).
- **R01** image build/publish for Hybrid; **R02** default branch `master`; **R03**/**R04** image tags (§8).
- **C01GO** Go 1.27 pinned; **C02GO** Go stated; **L01GO** logrus; **L02GO** outbound HTTP via resty.dev/v3 (§4.1).
- **N01, N02, N03, N04, N05, N06, N07, N08, N09, N10, N11, N12, N13, N14, N15, N16 (not applicable — public GitHub.com uses a standard public module path, S06), N17, N18, N19, N20 (vacuous — all tools `openWorldHint:false`), N21, N22, N23, N28, N30, N32** — all avoided/not violated (Go-profile MUST NOTs N01GO–N07GO likewise satisfied).
