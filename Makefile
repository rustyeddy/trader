APP     := trader
BIN_DIR := bin
BIN     := $(BIN_DIR)/$(APP)
CMD     := ./cmd

GOPATH  ?= $(shell go env GOPATH)
INSTALL_DIR := $(GOPATH)/bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags="-X github.com/rustyeddy/trader.Version=$(VERSION)"

TULIP_DIR ?= ../tulip

.PHONY: all build ui tulip-sync build-tulip build-full vet tidy test cover cover-html test-blackbox run live-portfolio smoke smoke-live smoke-live-dry sweep backtest-scalper install clean

all: vet build

# Build Go binary only (uses whatever is already in ui/dist/).
build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN) $(CMD)

# Build the SvelteKit UI, then rebuild the Go binary with fresh assets.
ui:
	cd ui && npm run build

# Copy the pre-built tulip dist into ui/dist/ and rebuild the Go binary.
# Override the source with: make tulip-sync TULIP_DIR=/path/to/tulip
tulip-sync:
	@test -d $(TULIP_DIR)/dist || (echo "error: $(TULIP_DIR)/dist not found — run 'npm run build' in tulip first" && exit 1)
	rsync -a --delete $(TULIP_DIR)/dist/ ui/dist/

# Build tulip, sync its dist, then rebuild the Go binary.
build-tulip:
	cd $(TULIP_DIR) && npm run build
	$(MAKE) tulip-sync build

build-full: ui build

vet:
	go vet ./...

tidy:
	go mod tidy

test:
	go test ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

cover-html:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

blackbox:
	go test ./... -tags=blackbox

run: build
	$(BIN)

live-portfolio: build
	$(BIN) live portfolio \
		--config /srv/trading/live/configs/demo-portfolio.yml \
		--log-level info \
		--log-format json \
		--log-file /srv/trading/live/logs/portfolio.log

smoke: build
	@scripts/smoke.sh

# Live OANDA integration smoke test — requires OANDA_TOKEN and an active market session.
# Runs the pulse strategy against a $2,000 practice account to exercise broker plumbing.
# Phase 2: uncomment GBP_USD block in testdata/configs/smoke-test.yml first.
smoke-live-dry: build
	$(BIN) live portfolio \
		--config testdata/configs/smoke-test.yml \
		--log-level info \
		--dry-run

smoke-live: build
	@mkdir -p logs
	$(BIN) live portfolio \
		--config testdata/configs/smoke-test.yml \
		--log-level info \
		--log-format json \
		--log-file logs/smoke-live.log

# Incremental scalper development — edit strategies/scalper/scalper.go then re-run.
# Uses 1 month of M1 data for a fast feedback loop (~seconds per run).
# Widen the date range in testdata/configs/scalper-backtest.yml once logic is stable.
backtest-scalper: build
	$(BIN) backtest run testdata/configs/scalper-backtest.yml

sweep:
	go test -tags sweep -timeout 15m -v -count=1 ./service/... -run TestStrategySweep

install: build
	cp $(BIN) $(INSTALL_DIR)/$(APP)

clean:
	@rm -rf $(BIN_DIR) coverage.out coverage.html ui/dist ui/.svelte-kit
