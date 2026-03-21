# syntax=docker/dockerfile:1
# Build context: repo root  (docker build -f docker/Dockerfile .)

# ─── Stage 1: build Go server ─────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Cache go modules before copying source
COPY packages/server/go.mod packages/server/go.sum ./
RUN go mod download

COPY packages/server/ ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o fafb .

# ─── Stage 2: runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# gpg for secret_* tools; git for github_* tools; ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates gpg git

WORKDIR /app
COPY --from=builder /build/fafb .

# The server reads config from CLAUDE_DIR (default /root/.claude inside container).
# Mount your host ~/.claude at /root/.claude and pass env vars via --env-file.
VOLUME ["/root/.claude"]

# HTTP transport — connect MCP clients to /mcp on this port
EXPOSE 8080

ENTRYPOINT ["./fafb", "--serve", ":8080"]
