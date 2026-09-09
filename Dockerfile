# Runtime image for the HYBRID mcp-forgejo MCP server.
#
# The binary is NOT built here — it is produced by GoReleaser in a separate CI
# step (see .github/workflows/images.yml) and copied in from the build context
# (dist/mcp-forgejo). The runtime image is distroless/static: the server is
# stateless and only talks to the remote Forgejo API over HTTP(S), so it needs
# no shell, package manager or certificates bundle beyond the minimal CA set.
#
# Build (context must contain dist/mcp-forgejo):
#   goreleaser build --snapshot --clean
#   docker build -t mcp-forgejo .

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY dist/mcp-forgejo /app/mcp-forgejo

# HTTP/SSE listener port (HOST/PORT config). Plain HTTP only — TLS is always
# terminated at the reverse proxy in front of this container.
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/mcp-forgejo"]
