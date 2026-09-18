# =============================================================================
# mcp-forgejo — Makefile (Go profile)
#
# Go targets used by CI. The CI `e2e` job calls `make test-e2e` (T02/C04;
# Go: C09GO). e2e tests run through the github.com/teran/go-docker-testsuite
# harness and are build-tagged (`//go:build e2e`) so they are excluded from the
# default unit run (`make test` / `go test ./...`).
# =============================================================================

SERVER ?= mcp-forgejo           # binary name (see cmd/mcp-forgejo)

.PHONY: test test-race test-e2e test-e2e-cover build lint arch vet fmt clean

## Unit tests (default run) — excludes e2e (build tag `e2e`).
test:
	go test ./...

## Unit tests with the race detector (CI: C04GO).
test-race:
	go test -race ./...

## e2e tests via the go-docker-testsuite harness (github.com/teran/go-docker-testsuite).
## Requires a running Docker daemon. Files carry `//go:build e2e` so they are NOT
## part of the default `go test ./...`. The CI `e2e` job invokes this target.
test-e2e:
	go test -tags e2e ./...

## e2e tests with a separate coverage profile, so the e2e layer's coverage of the
## stack can be measured independently of the unit run (C01GO unit profile is
## cover.out). Inspect with: go tool cover -func=e2e-cover.out | awk '/^total:/ {print $3}'
test-e2e-cover:
	go test -tags e2e -coverprofile=e2e-cover.out -covermode=atomic ./...

## Build the server binary into dist/.
build:
	go build -o dist/$(SERVER) ./cmd/$(SERVER)

## Optional — lint via golangci-lint (CI: C03GO).
lint:
	golangci-lint run ./...

## Optional — enforce DDD/Clean layer edges (CI: C07GO).
arch:
	go-arch-lint check

## Optional — go vet.
vet:
	go vet ./...

## Optional — gofmt/gofumpt formatting check.
fmt:
	gofumpt -l .

## Optional — clean build artifacts.
clean:
	rm -rf dist/
