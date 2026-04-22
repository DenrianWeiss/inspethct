BIN_DIR  := bin
PLUGIN   := plugin
VSIX_OUT := $(BIN_DIR)

# Build flags — embed version from git tag when available.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: build build-all build-vsix release test test-all clean

## build: compile the host binary for the current platform.
build:
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd ./cmd/inspethctd

## build-all: cross-compile binaries for all supported platforms.
build-all:
	mkdir -p $(BIN_DIR)
	GOOS=linux  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd-linux-amd64   ./cmd/inspethctd
	GOOS=linux  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd-linux-arm64   ./cmd/inspethctd
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd-darwin-amd64  ./cmd/inspethctd
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd-darwin-arm64  ./cmd/inspethctd
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/inspethctd-windows-amd64.exe ./cmd/inspethctd

## build-vsix: compile the VS Code extension and package it as a .vsix file.
build-vsix:
	mkdir -p $(VSIX_OUT)
	cd $(PLUGIN) && npm ci --prefer-offline
	cd $(PLUGIN) && npm run compile
	cd $(PLUGIN) && npx vsce package --out ../$(VSIX_OUT)/inspethct-$(VERSION).vsix --no-git-tag-version

## release: build all platform binaries and the VSIX in one step.
release: build-all build-vsix

## test: run the core backend test suites (fast, no network required).
test:
	go test ./internal/engine/... ./internal/jsonrpc/... ./internal/contractmeta/... ./internal/srcmap/...

## test-all: run the full backend test suite.
test-all:
	go test -count=1 ./...

## clean: remove built artifacts.
clean:
	rm -rf $(BIN_DIR)/inspethctd*
	rm -f  $(VSIX_OUT)/inspethct-*.vsix
	cd $(PLUGIN) && rm -rf out

install-plugin:
	code --install-extension $(VSIX_OUT)/inspethct-$(VERSION).vsix

local-update: build build-vsix install-plugin