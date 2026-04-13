.PHONY: build test lint extract query setup test-compat test-taint test-golden test-all-integration test-bench golden-check

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

test-compat:
	go test -run TestCompat -timeout 120s -count=1 ./...

test-taint:
	go test -run TestV2 -timeout 120s -count=1 ./...

test-golden:
	go test -run TestGolden -timeout 120s -count=1 ./...

test-all-integration:
	go test -run 'TestCompat|TestV2|TestGolden' -timeout 180s -count=1 ./...

test-bench:
	go test -bench=. -benchtime=3s -timeout 120s ./...

golden-check:
	go test -run 'TestCompat|TestGolden' -update -count=1 ./...
	git diff --exit-code testdata/
