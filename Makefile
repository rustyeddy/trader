APP     := trader
BIN_DIR := bin
BIN     := $(BIN_DIR)/$(APP)
CMD     := ./cmd

GOPATH  ?= $(shell go env GOPATH)
INSTALL_DIR := $(GOPATH)/bin

.PHONY: all build ui build-full vet tidy test cover cover-html test-blackbox run install clean

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

install: build
	cp $(BIN) $(INSTALL_DIR)/$(APP)

clean:
	@rm -rf $(BIN_DIR) coverage.out coverage.html ui/dist ui/.svelte-kit
