# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o caboose-mcp .

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/caboose-mcp .

ENTRYPOINT ["/app/caboose-mcp"]
# Default: stdio MCP server. Override in ECS task def:
#   --serve :8080   → HTTP transport
#   --bots          → Slack + Discord bots
