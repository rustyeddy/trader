# ── Stage 1: build the UI ────────────────────────────────────────────────────
# Always runs on the build machine (amd64 in CI), never emulated.
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ── Stage 2: build the Go binary ─────────────────────────────────────────────
# Also runs on the build machine. Go cross-compiles natively via GOARCH/GOOS,
# so no QEMU is needed for the default (CGO_ENABLED=0) build.
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder

# Injected by docker buildx for each target platform.
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /ui/dist ./ui/dist

ARG VERSION=dev
# SQLITE=1 enables the sqlite build tag (requires CGo).
# Cross-compiling CGo needs a cross-compiler toolchain; use QEMU instead for
# that case. The default (SQLITE=0) cross-compiles cleanly with no QEMU.
ARG SQLITE=0

RUN if [ "$SQLITE" = "1" ]; then \
      CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -tags sqlite \
        -ldflags="-s -w -X github.com/rustyeddy/trader.Version=${VERSION}" \
        -o /trader ./cmd/main.go; \
    else \
      CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -ldflags="-s -w -X github.com/rustyeddy/trader.Version=${VERSION}" \
        -o /trader ./cmd/main.go; \
    fi

# ── Stage 3a: default runtime — static, no libc, Pi/cloud/desktop ────────────
FROM gcr.io/distroless/static-debian12 AS runtime

COPY --from=builder /trader /usr/local/bin/trader
VOLUME ["/data/candles", "/var/lib/trader"]
ENTRYPOINT ["trader"]
CMD ["--help"]

# ── Stage 3b: SQLite runtime — needs libc for go-sqlite3 ─────────────────────
# docker build --build-arg SQLITE=1 --target runtime-sqlite .
FROM debian:bookworm-slim AS runtime-sqlite

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /trader /usr/local/bin/trader
VOLUME ["/data/candles", "/var/lib/trader"]
ENTRYPOINT ["trader"]
CMD ["--help"]
