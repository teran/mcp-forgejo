# Multi-stage build for the HYBRID mcp-forgejo MCP server.
# The runtime image is distroless/scratch: the server is stateless and only
# talks to the remote Forgejo API over HTTP(S), so it needs no shell, package
# manager or certificates bundle beyond the minimal CA set.

# ---- Build stage ----
FROM golang:1.27 AS build

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum* ./
RUN go mod download

# Copy the whole tree and build a static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcp-forgejo ./cmd/mcp-forgejo

# ---- Runtime stage ----
# distroless/static provides a CA bundle and an empty base; scratch would work
# too but distroless keeps common root certs for outgoing HTTPS to Forgejo.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/mcp-forgejo /app/mcp-forgejo

# HTTP/SSE listener port (HOST/PORT config). Plain HTTP only — TLS is always
# terminated at the reverse proxy in front of this container.
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/mcp-forgejo"]
