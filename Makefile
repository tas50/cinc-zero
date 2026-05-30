# cinc-zero build automation

BINARY      := cinc-zero
CMD_PKG     := ./cmd/cinc-zero
LDFLAGS_PKG := main
DIST_DIR    ?= dist
PLATFORMS   := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Build metadata — override on the command line if needed.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%d)

LDFLAGS := -X $(LDFLAGS_PKG).version=$(VERSION) \
           -X $(LDFLAGS_PKG).commit=$(COMMIT) \
           -X $(LDFLAGS_PKG).buildDate=$(BUILD_DATE)

.PHONY: all build dist install test vet fmt tidy clean run help

all: build

## build: compile the cinc-zero binary with version metadata
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD_PKG)

## dist: build release archives for Linux and macOS
dist:
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; \
		goarch=$${platform#*/}; \
		name="$(BINARY)_$(VERSION)_$${goos}_$${goarch}"; \
		mkdir -p "$(DIST_DIR)/$$name"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build -trimpath -ldflags "$(LDFLAGS) -s -w" -o "$(DIST_DIR)/$$name/$(BINARY)" $(CMD_PKG); \
		tar -C "$(DIST_DIR)" -czf "$(DIST_DIR)/$$name.tar.gz" "$$name"; \
		rm -rf "$(DIST_DIR)/$$name"; \
	done
	cd "$(DIST_DIR)" && shasum -a 256 *.tar.gz > SHA256SUMS

## install: install cinc-zero into the Go bin directory
install:
	go install -ldflags "$(LDFLAGS)" $(CMD_PKG)

## test: run the test suite
test:
	go test ./... -race -cover

## vet: run go vet across all packages
vet:
	go vet ./...

## fmt: format all Go source
fmt:
	gofmt -w .

## tidy: tidy go module dependencies
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BINARY) $(DIST_DIR)
	go clean

## run: build and run cinc-zero (pass flags via ARGS="...")
run: build
	./$(BINARY) $(ARGS)

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
