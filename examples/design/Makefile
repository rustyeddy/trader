BIN := bin/design

.PHONY: all check test vet build run clean

all: check build

# vet + test, the quick verification gate
check: vet test

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o $(BIN) .

run:
	go run .

clean:
	rm -rf bin
