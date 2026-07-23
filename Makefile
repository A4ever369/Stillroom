.PHONY: build test vet fmt still stillroomd serve eval eval-list

build:
	go build ./...

still:
	go build -o bin/still ./cmd/still

# The self-hostable server: org-wide search over every repo's knowledge.
stillroomd:
	go build -o bin/stillroomd ./cmd/stillroomd

# Run it against everything you have checked out — the fastest way to see
# whether cross-repo search is worth anything on your own knowledge.
serve: stillroomd
	./bin/stillroomd -scan $(HOME)/code

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
