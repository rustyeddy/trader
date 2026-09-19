# LINT_VERSION is pinned so `make lint` behaves identically for every
# contributor and in CI, rather than silently changing behavior when a
# new golangci-lint release adds or reconfigures linters.
LINT_VERSION := v2.13.0
# PROTOC_GEN_GO_VERSION/PROTOC_GEN_GO_GRPC_VERSION are pinned for the
# same reason (review finding on PR #387: an unpinned `@latest` makes
# `make gen-proto` non-reproducible — a later run could regenerate
# protocol/strategy/v1's committed bindings with a different generator
# than actually produced them). These match the versions recorded in
# strategy.pb.go/strategy_grpc.pb.go's own "// versions:" header
# comments; bump both together with a regeneration when either changes.
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2
COVERPROFILE := coverage.out
BINARY := trader
BIN_DIR := bin

# GIT_DESCRIBE/LDFLAGS (issue #389, ADR-064): the build/install targets
# below inject the current `git describe --tags --always --dirty`
# output into cmd/trader/internal/version's own gitDescribe var — the
# primary version-derivation mechanism, replacing ADR-046's earlier
# hand-maintained Version const. No manual edit is required solely
# because a new tag was created; see that package's own doc comment
# for the complete precedence/fallback chain this enables (in
# particular, for a plain `go build ./cmd/trader` run outside `make`,
# which never sees these ldflags at all). The `2>/dev/null || true`
# guards a build attempted outside any git repository (or before the
# very first commit) from failing outright merely because git describe
# itself has nothing to report — gitDescribe is simply empty in that
# case, and version.Current() falls back accordingly.
GIT_DESCRIBE := $(shell git describe --tags --always --dirty 2>/dev/null || true)
VERSION_PKG := github.com/rustyeddy/trader/cmd/trader/internal/version
LDFLAGS := -X $(VERSION_PKG).gitDescribe=$(GIT_DESCRIBE)

all: check

# build compiles the trader CLI (cmd/trader) to ./bin/trader.
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/trader

# install builds and installs trader via `go install`, the standard Go
# convention: it lands in $GOBIN, or $GOPATH/bin (~/go/bin by default)
# when GOBIN is unset, and requires no elevated privileges.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/trader

# gen-proto regenerates protocol/strategy/v1's Go bindings from
# strategy.proto (issue #377, ADR-062). Installs protoc-gen-go/
# protoc-gen-go-grpc at the pinned versions above (go install itself
# is idempotent and fast when already at that version, mirroring
# `lint`'s own `go run ...@$(LINT_VERSION)` pinning convention) — only
# protoc itself (the C++ compiler binary, not a Go module `go install`
# can pin) must already be on PATH; these bindings were last generated
# with protoc v29.3, also recorded in the generated files' own
# "// versions:" header comments.
#
# Not part of `make check`/`make build`: generated output is committed
# (protocol/strategy/v1/strategy.pb.go, strategy_grpc.pb.go), so a
# normal build/test/CI run never needs protoc installed at all — only
# a contributor editing strategy.proto itself runs this target.
gen-proto:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	protoc \
		--proto_path=protocol/strategy/v1 \
		--go_out=protocol/strategy/v1 --go_opt=paths=source_relative \
		--go-grpc_out=protocol/strategy/v1 --go-grpc_opt=paths=source_relative \
		protocol/strategy/v1/strategy.proto

fmt:
	go fmt ./...

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION) run ./...

# coverage runs the full test suite with coverage instrumentation and
# prints a per-function report, matching AGENTS.md's 85%-or-greater
# coverage goal.
coverage:
	go test -coverprofile=$(COVERPROFILE) ./...
	go tool cover -func=$(COVERPROFILE)

# coverage-html renders the same profile as a browsable HTML report.
coverage-html: coverage
	go tool cover -html=$(COVERPROFILE) -o coverage.html

check: fmt-check vet lint test race

.PHONY: all build install gen-proto fmt fmt-check vet test race lint coverage coverage-html check
