VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/atop0914/llmtrace/internal/version.Version=$(VERSION) \
	-X github.com/atop0914/llmtrace/internal/version.GitCommit=$(COMMIT) \
	-X github.com/atop0914/llmtrace/internal/version.BuildDate=$(DATE)

.PHONY: test lint build bench vet ci clean

test:
	go test -short -v -race -count=1 ./...

bench:
	go test -bench=. -benchmem ./... -count=1

lint:
	golangci-lint run

build:
	go build -ldflags="$(LDFLAGS)" -o bin/llmtrace ./examples/basic

vet:
	go vet ./...

ci: vet lint test bench

clean:
	rm -rf bin/ dist/
