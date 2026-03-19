# syntax=docker/dockerfile:1
# Build context: repo root  (docker build -f docker/Dockerfile .)

# ─── Stage 1: build React UI ──────────────────────────────────────────────────
FROM node:20-alpine AS ui-builder

WORKDIR /ui-src
RUN corepack enable && corepack prepare pnpm@latest --activate

# Install deps (no frozen-lockfile — lock file not committed)
COPY packages/ui/package.json ./
RUN pnpm install --no-frozen-lockfile

COPY packages/ui/ ./
RUN pnpm build

# ─── Stage 2: build Go server ─────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Cache go modules before copying source
COPY packages/server/go.mod packages/server/go.sum ./
RUN go mod download

COPY packages/server/ ./

# Embed the built React app — go:embed ui/dist expects this directory
COPY --from=ui-builder /ui-src/dist/ ./ui/dist/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o caboose-mcp .

# ─── Stage 3: runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# gpg for secret_* tools; git for github_* tools; ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates gpg git

WORKDIR /app
COPY --from=builder /build/caboose-mcp .

# The server reads config from CLAUDE_DIR (default /root/.claude inside container).
# Mount your host ~/.claude at /root/.claude and pass env vars via --env-file.
VOLUME ["/root/.claude"]

# HTTP transport — connect MCP clients to /mcp on this port
EXPOSE 8080

ENTRYPOINT ["./caboose-mcp", "--serve", ":8080"]
