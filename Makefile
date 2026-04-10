.PHONY: build test lint extract query

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
