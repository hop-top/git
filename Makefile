BINARY_NAME=git-hop
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all build clean test lint fmt

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) main.go

run: build
	./$(BINARY_NAME)

clean:
	go clean
	rm -f $(BINARY_NAME)

test:
	go test -v ./internal/...

# Requires staticcheck installed: go install honnef.co/go/tools/cmd/staticcheck@latest
lint:
	go vet ./...
	staticcheck ./...

fmt:
	go fmt ./...
