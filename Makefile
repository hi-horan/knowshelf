APP_NAME := knowshelf
OUTPUT_DIR := _output
CMD_DIR := ./cmd/knowshelf
GO ?= go
GOFMT ?= gofmt
GIT_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
GIT_TAG ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION_LDFLAGS := -X main.gitVersion=$(GIT_VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitTag=$(GIT_TAG)


.PHONY: build
build:
	mkdir -p $(OUTPUT_DIR)
	$(GO) build -ldflags "$(VERSION_LDFLAGS)" -o $(OUTPUT_DIR)/$(APP_NAME) $(CMD_DIR)

.PHONY: run
run:
	$(GO) run -ldflags "$(VERSION_LDFLAGS)" $(CMD_DIR) run -c config.yaml

.PHONY: import
import:
	$(GO) run -ldflags "$(VERSION_LDFLAGS)" $(CMD_DIR) import xx.md -c config.yaml

.PHONY: embed
embed:
	$(GO) run -ldflags "$(VERSION_LDFLAGS)" $(CMD_DIR) embed -c config.yaml --limit=1000

.PHONY: embed_show
embed_show:
	$(GO) run -ldflags "$(VERSION_LDFLAGS)" $(CMD_DIR) embed show -c config.yaml -n=1

.PHONY: embed_export
embed_export:
	$(GO) run -ldflags "$(VERSION_LDFLAGS)" $(CMD_DIR) embed export -c config.yaml --out _output/projector --limit 5000

.PHONY: token
token:
	$(GO) run $(CMD_DIR) token_gen -c config.yaml --sub codex --scope mcp:read --ttl 24000h

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GOFMT) -w cmd internal

clean:
	rm -rf $(OUTPUT_DIR)
