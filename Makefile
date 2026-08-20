# LINT_VERSION is pinned so `make lint` behaves identically for every
# contributor and in CI, rather than silently changing behavior when a
# new golangci-lint release adds or reconfigures linters.
LINT_VERSION := v2.13.0
COVERPROFILE := coverage.out
BINARY := trader
BIN_DIR := bin

all: check

# build compiles the trader CLI (cmd/trader) to ./bin/trader.
build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/trader

# install builds and installs trader via `go install`, the standard Go
# convention: it lands in $GOBIN, or $GOPATH/bin (~/go/bin by default)
# when GOBIN is unset, and requires no elevated privileges.
install:
	go install ./cmd/trader

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

.PHONY: all build install fmt fmt-check vet test race lint coverage coverage-html check
