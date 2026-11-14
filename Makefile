BIN := bin/trader

# LINT_VERSION is pinned so `make lint` behaves identically for every
# contributor and in CI, rather than silently changing behavior when a
# new golangci-lint release adds or reconfigures linters.
LINT_VERSION := v2.13.0

.PHONY: all check tidy-check fmt-check test race cover vet lint smoke build run clean

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

# smoke runs the opt-in live OANDA check (not part of `make check`). It needs
# TRADER_OANDA_TOKEN in your shell (never on a command line) and writes only to a
# temp directory. For a live-account token also set
# TRADER_OANDA_BASE_URL=https://api-fxtrade.oanda.com.
# The full output goes to bin/smoke.log; stdout gets only the verdict.
SMOKE_LOG := bin/smoke.log

smoke:
	@mkdir -p bin
	@go test -tags oandasmoke ./cmd/trader -run TestSmokeOANDA -v -count=1 > $(SMOKE_LOG) 2>&1; rc=$$?; \
	if [ $$rc -eq 0 ]; then echo "smoke: PASS (log: $(SMOKE_LOG))"; else echo "smoke: FAIL (log: $(SMOKE_LOG))"; fi; \
	exit $$rc

build:
	go build -o $(BIN) ./cmd/trader

run:
	go run ./cmd/trader

clean:
	rm -rf bin
