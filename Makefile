VERSION  ?= $(shell git describe --tags --always)
COMMIT   ?= $(shell git rev-parse --short HEAD)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
BIN      := $(HOME)/go/bin/agent-mesh

.PHONY: build install test lint clean release-dry

build:
	go build -ldflags "$(LDFLAGS)" -o agent-mesh ./cmd/agent-mesh

install:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/agent-mesh
	@echo "installed $(BIN)"

test:
	go test ./... -race -count=1

lint:
	@which golangci-lint > /dev/null 2>&1 || echo "install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
	golangci-lint run ./...

clean:
	rm -f agent-mesh
	go clean -cache

release-dry:
	goreleaser release --snapshot --clean
