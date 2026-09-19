# =============================================================================
# mcp-forgejo — Makefile (Go profile)
#
# Build-system interface (R07/N31). CI binds to these make targets instead of
# raw commands; a CI job must never call an undeclared optional target.
#
#   CORE (CI binds to these):
#     make lint             golangci-lint + go vet + gofumpt formatting check
#     make test             unit tests (-race) + coverage profile + >= 95% gate
#     make build            build the server binary into dist/ (go build)
#     make e2e              e2e tests via go-docker-testsuite (build tag `e2e`)
#
#   OPTIONAL (declared; CI may not call them unless it declares them):
#     make container-image  goreleaser build + docker build; push=true to push
#     make e2e-cover        e2e with a separate coverage profile
#     make arch             go-arch-lint dependency edges
#     make vet              go vet only
#     make fmt              gofumpt formatting check only
#     make clean            remove build artifacts
#
# e2e tests run through the github.com/teran/go-docker-testsuite harness and
# are build-tagged (`//go:build e2e`) so they are excluded from the default
# unit run (`make test` / `go test ./...`). `container-image` is applicable
# because this is a HYBRID server (stdio + HTTP/SSE) that MUST publish an
# image (B03/R01); the binary is produced by GoReleaser (B04 — the Dockerfile
# only copies dist/mcp-forgejo, it does NOT recompile).
# =============================================================================

SERVER ?= mcp-forgejo           # binary name (see cmd/mcp-forgejo)
# `make container-image push=true` also pushes the image.
push ?= false

.PHONY: test lint build e2e e2e-cover container-image arch vet fmt clean

## Core — unit tests with the race detector + coverage profile + hard >= 95%
## coverage gate (CI: C1/C3, Go: C09GO). Excludes e2e (build tag `e2e`).
test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...
	@total=$$(go tool cover -func=cover.out | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	echo "total coverage: $${total}%"; \
	awk -v t="$${total}" 'BEGIN { if (t+0 < 95) { printf "ERROR: coverage %s%% is BELOW the 95%% threshold — build failed.\n", t; exit 1 } else { printf "OK: coverage %s%% meets the >= 95%% threshold.\n", t } }'

## Core — lint: golangci-lint + go vet (gofumpt formatting is included by
## golangci-lint; the standalone `fmt` target is kept for convenience).
lint:
	golangci-lint run ./...
	go vet ./...

## Core — build the server binary into dist/.
build:
	go build -o dist/$(SERVER) ./cmd/$(SERVER)

## Core — e2e tests via the go-docker-testsuite harness (github.com/teran/go-docker-testsuite).
## Requires a running Docker daemon. Files carry `//go:build e2e` so they are NOT
## part of the default `go test ./...`. The CI `e2e` job invokes this target.
e2e:
	go test -tags e2e ./...

## Optional — e2e tests with a separate coverage profile, so the e2e layer's
## coverage of the stack can be measured independently of the unit run (the
## unit profile is cover.out). Inspect with: go tool cover -func=e2e-cover.out | awk '/^total:/ {print $3}'
e2e-cover:
	go test -tags e2e -coverprofile=e2e-cover.out -covermode=atomic ./...

## Optional — build the container image (Hybrid → R1). GoReleaser produces the
## single binary artifact dist/mcp-forgejo (B4 — the Dockerfile only copies it
## in, no in-image compile), then `docker build`. `make container-image
## push=true` also pushes the image.
container-image:
	goreleaser build --snapshot --clean --single-target --output dist/mcp-forgejo
	docker build -t mcp-forgejo .
	@if [ "$(push)" = "true" ]; then \
		echo "Pushing mcp-forgejo ..."; \
		docker push mcp-forgejo; \
	fi

## Optional — enforce DDD/Clean layer edges (CI: C06GO).
arch:
	go-arch-lint check

## Optional — go vet (also folded into `make lint`).
vet:
	go vet ./...

## Optional — gofumpt formatting check (also folded into `make lint`).
fmt:
	gofumpt -l .

## Optional — clean build artifacts.
clean:
	rm -rf dist/
