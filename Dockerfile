# ── Stage 1: build the UI ────────────────────────────────────────────────────
FROM node:22-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ── Stage 2: build the Go binary ─────────────────────────────────────────────
FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /ui/dist ./ui/dist

ARG VERSION=dev
# SQLITE=1 enables the sqlite build tag (CGo required; used for local/dev runs).
# Default is 0: fully static binary, no CGo, suitable for Pi and cloud.
ARG SQLITE=0

RUN if [ "$SQLITE" = "1" ]; then \
      CGO_ENABLED=1 go build \
        -tags sqlite \
        -ldflags="-s -w -X github.com/rustyeddy/trader.Version=${VERSION}" \
        -o /trader ./cmd/main.go; \
    else \
      CGO_ENABLED=0 go build \
        -ldflags="-s -w -X github.com/rustyeddy/trader.Version=${VERSION}" \
        -o /trader ./cmd/main.go; \
    fi

# ── Stage 3a: default runtime — static, no libc, Pi/cloud/desktop ────────────
# Pulled from the registry on a Pi; no build needed on-device.
FROM gcr.io/distroless/static-debian12 AS runtime

COPY --from=builder /trader /usr/local/bin/trader
VOLUME ["/data/candles", "/var/lib/trader"]
ENTRYPOINT ["trader"]
CMD ["--help"]

# ── Stage 3b: SQLite runtime — needs libc for go-sqlite3 ─────────────────────
# Build with: docker build --build-arg SQLITE=1 --target runtime-sqlite .
FROM debian:bookworm-slim AS runtime-sqlite

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /trader /usr/local/bin/trader
VOLUME ["/data/candles", "/var/lib/trader"]
ENTRYPOINT ["trader"]
CMD ["--help"]
