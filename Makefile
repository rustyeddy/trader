BIN := bin/trader

# LINT_VERSION is pinned so `make lint` behaves identically for every
# contributor and in CI, rather than silently changing behavior when a
# new golangci-lint release adds or reconfigures linters.
LINT_VERSION := v2.13.0

.PHONY: all check tidy-check fmt-check test race cover vet lint build run clean

all: check build

# The verification gate: the same steps, in the same order, as CI.
# The gates run one after another even under `make -j`, so a later gate never
# runs when an earlier one fails.
check:
	$(MAKE) tidy-check
	$(MAKE) fmt-check
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) race

# tidy-check fails when go.mod/go.sum are not what `go mod tidy` would
# produce, without modifying them. CI runs this same target.
tidy-check:
	go mod tidy -diff

fmt-check:
	@out=$$(gofmt -l .) || exit 1; \
	if [ -n "$$out" ]; then echo "gofmt needs to run on:"; echo "$$out"; exit 1; fi

test:
	go test ./...

race:
	go test -race ./...

cover:
	mkdir -p bin
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
