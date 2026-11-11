BIN := bin/trader

.PHONY: all check test race cover vet build run clean

all: check build

# vet + test + race, the verification gate
check: vet test race

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=bin/cover.out ./... && go tool cover -func=bin/cover.out

vet:
	go vet ./...

build:
	go build -o $(BIN) ./cmd/trader

run:
	go run ./cmd/trader

clean:
	rm -rf bin
