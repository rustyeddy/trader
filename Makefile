BIN := bin/trader

# LINT_VERSION is pinned so `make lint` behaves identically for every
# contributor and in CI, rather than silently changing behavior when a
# new golangci-lint release adds or reconfigures linters.
LINT_VERSION := v2.13.0

.PHONY: all check tidy-check fmt-check test race cover vet lint build run clean

all: check build

# The verification gate: the same steps, in the same order, as CI.
check: tidy-check fmt-check vet lint test race

# tidy-check fails when go.mod/go.sum are not what `go mod tidy` would
# produce, without modifying them. CI runs this same target.
tidy-check:
	go mod tidy -diff

fmt-check:
	test -z "$$(gofmt -l .)"

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=bin/cover.out ./... && go tool cover -func=bin/cover.out

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION) run ./...

build:
	go build -o $(BIN) ./cmd/trader

run:
	go run ./cmd/trader

clean:
	rm -rf bin
