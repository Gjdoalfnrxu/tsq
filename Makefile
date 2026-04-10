.PHONY: build test lint extract query setup

build:
	go build -o bin/tsq ./cmd/tsq

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

extract:
	go run ./cmd/tsq extract $(ARGS)

query:
	go run ./cmd/tsq query $(ARGS)

setup:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
