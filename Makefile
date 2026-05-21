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

.PHONY: all build run test cover lint tidy clean docker help web-install web-build web-dev web-test

all: lint test build

build: web-build ## Build the binary into ./bin (after building the React UI)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD)

# ---- Frontend (M6) ----

web-install: ## Install JS deps via bun
	cd web && bun install --frozen-lockfile

web-build: ## Build the React UI into web/dist (embedded by web/web.go)
	cd web && bun install --frozen-lockfile && bun run build

web-dev: ## Run the Vite dev server against a running daemon
	cd web && bun run dev

web-test: ## Run vitest
	cd web && bun run test

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
