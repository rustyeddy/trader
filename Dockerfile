# ── Stage 1: build the UI ────────────────────────────────────────────────────
FROM node:22-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ── Stage 2: build the Go binary ─────────────────────────────────────────────
# golang image includes gcc, needed for go-sqlite3 (CGo).
FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /ui/dist ./ui/dist

# CGO_ENABLED=1 is required for go-sqlite3.
# When building for linux/arm64 via "docker buildx --platform linux/arm64",
# the build runs under QEMU — no cross-compiler toolchain needed, but the
# arm64 build will take several minutes on an amd64 host.
ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
      -ldflags="-s -w -X github.com/rustyeddy/trader.Version=${VERSION}" \
      -o /trader \
      ./cmd/main.go

# ── Stage 3: minimal runtime ─────────────────────────────────────────────────
# debian:bookworm-slim instead of scratch because go-sqlite3 links against libc.
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /trader /usr/local/bin/trader

# Candle data and the SQLite journal live on mounted volumes; these are the
# conventional paths the default config expects.
VOLUME ["/data/candles", "/var/lib/trader"]

ENTRYPOINT ["trader"]
CMD ["--help"]
