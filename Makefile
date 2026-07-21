.PHONY: build test vet fmt still eval eval-list

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

# L5 distillation-quality eval (docs/testing.md L5). Spends real tokens via
# your `claude -p`; NOT part of `make test` or CI. Run before/after a
# BuildPrompt change to see whether quality moved.
eval:
	go run ./cmd/eval

# Token-free: list the corpus cases that would be evaluated.
eval-list:
	go run ./cmd/eval -list
