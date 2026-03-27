APP     := trader
BIN_DIR := bin
BIN     := $(BIN_DIR)/$(APP)
CMD     := ./cmd

GOPATH  ?= $(shell go env GOPATH)
INSTALL_DIR := $(GOPATH)/bin

.PHONY: all build vet tidy test cover cover-html test-blackbox run install clean

all: vet build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) $(CMD)

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
	@rm -rf $(BIN_DIR) coverage.out coverage.html
