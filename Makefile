APP := trader
BIN_DIR := bin
BIN := $(BIN_DIR)/$(APP)
CMD := cmd/main.go

.PHONY: build test cover cover-html clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) $(CMD)

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

install: build
	cp $(CMD) /home/rusty/bin/$(APP)


clean:
	@rm -rf $(BIN_DIR) coverage.out coverage.html
