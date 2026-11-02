# Security Policy

This MCP server is developed and tested largely by AI agents with minimal human
review, so security is enforced automatically by CI (lint, coverage, mutation,
gosec, govulncheck, gitleaks). The security baseline is described in `SPEC.md`
§5 (S01–S14) — the single source of truth for the security contracts this
repository must honour.

## Reporting a Vulnerability

Please **do not** open a public issue for a security vulnerability.

Report vulnerabilities privately to the maintainers at
[me@teran.dev](mailto:me@teran.dev).

Please include:

- the affected version(s) and how you detected the issue;
- a minimal, reproducible description of the vulnerability;
- the potential impact and, if known, a suggested fix;
- the deployment context in which it occurs (stdio vs HTTP/SSE, reverse proxy
  configuration), if relevant.

## What happens next

1. The maintainers acknowledge the report within **5** business days.
2. The issue is triaged: severity, affected versions, and a fix plan.
3. A fix is developed **test-first** (TDD) and lands through the normal CI gates
   (coverage ≥ 95%, gremlins, gosec/govulncheck fixed-not-suppressed).
4. Once a fix is available, the vulnerability is disclosed and (if appropriate)
   a `v*` release is published.

We ask that you **do not** disclose the vulnerability publicly until a fix has
been released or the maintainers explicitly agree otherwise (responsible
disclosure).

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | ✅ |
| Go 1.27.x (toolchain) | ✅ |

Only the latest release line is supported. Older versions are not patched;
upgrade to a supported version.

## Security Baseline

The authoritative security baseline is `SPEC.md` §5 (S01–S14). Key contracts:

- **TLS** is never implemented inside the server — it is terminated at the
  reverse proxy (S01).
- **Secrets** (`FORGEJO_TOKEN` and any credentials) are never leaked into tool
  output, logs, or errors — redaction is centralized in shared helpers (S02).
- **No local filesystem access** — the server only talks to the remote Forgejo
  API over HTTP, so there is no `ALLOW_DIRS` to scope (S04).
- **Tools are grouped read → write → delete**; destructive tools are annotated
  and require explicit confirmation (S03, S12).
- **HTTP/SSE deployments MUST sit behind a reverse proxy** that enforces client
  authentication/authorization, since the MCP listener offers no auth of its own
  (S07).
- **Findings are fixed, not suppressed** — gosec/govulncheck findings block the
  build rather than being silenced (S05).

See `SPEC.md` §5 for the full domain-specific security audit.
