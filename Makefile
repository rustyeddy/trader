APP     := trader
BIN_DIR := bin
BIN     := $(BIN_DIR)/$(APP)
CMD     := ./cmd

GOPATH  ?= $(shell go env GOPATH)
INSTALL_DIR := $(GOPATH)/bin

.PHONY: all build ui build-full vet tidy test cover cover-html test-blackbox run live-portfolio smoke smoke-live smoke-live-dry sweep install clean

all: vet build

# Build Go binary only (uses whatever is already in ui/dist/).
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) $(CMD)

# Build the SvelteKit UI, then rebuild the Go binary with fresh assets.
ui:
	cd ui && npm run build

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

test-blackbox:
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

sweep:
	go test -tags sweep -timeout 15m -v -count=1 ./service/... -run TestStrategySweep

install: build
	cp $(BIN) $(INSTALL_DIR)/$(APP)

clean:
	@rm -rf $(BIN_DIR) coverage.out coverage.html ui/dist ui/.svelte-kit
