BINARY_NAME=git-hop
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Platform-agnostic install path
GOBIN ?= $(shell go env GOPATH)/bin

.PHONY: all build clean test lint fmt install lint-links

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME)

install: build
	mkdir -p $(GOBIN)
	cp $(BINARY_NAME) $(GOBIN)/

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

# Requires lychee installed: brew install lychee (or cargo install lychee)
lint-links:
	lychee --offline --no-progress --exclude-path vendor 'docs/**/*.md' 'internal/**/*.md' '*.md'
