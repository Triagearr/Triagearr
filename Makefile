BINARY      := triagearr
PKG         := github.com/Triagearr/Triagearr
CMD         := ./cmd/triagearr
BIN_DIR     := bin
COVER_OUT   := coverage.out

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
               -X main.version=$(VERSION) \
               -X main.commit=$(COMMIT) \
               -X main.date=$(DATE)

GO          ?= go
GOFLAGS     ?=

.PHONY: all build run test cover lint tidy clean docker help

all: lint test build

build: ## Build the binary into ./bin
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD)

run: ## Run the binary directly
	$(GO) run $(CMD) $(ARGS)

test: ## Run unit tests
	$(GO) test $(GOFLAGS) -race -count=1 ./...

cover: ## Run tests with coverage report
	$(GO) test $(GOFLAGS) -race -count=1 -coverprofile=$(COVER_OUT) ./...
	$(GO) tool cover -func=$(COVER_OUT) | tail -n 1

lint: ## Run golangci-lint
	golangci-lint run ./...

tidy: ## go mod tidy
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(COVER_OUT) dist/

docker: ## Build docker image locally via goreleaser snapshot
	goreleaser release --snapshot --clean --skip=publish

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
