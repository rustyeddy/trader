all: check

fmt:
	go fmt ./...

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

check: fmt-check vet test race
