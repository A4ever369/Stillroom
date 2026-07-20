.PHONY: build test vet fmt still

build:
	go build ./...

still:
	go build -o bin/still ./cmd/still

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
