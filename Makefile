# Development tasks for jw-cli. Bare `make` builds; `make help` lists targets.

MODULE     := github.com/dgrieser/jw-cli
BINARY     := jw
MAIN_PKG   := ./cmd/jw
DIST_DIR   := dist
COVER_FILE := coverage.out
COVER_HTML := coverage.html

GO       ?= go
# Left empty on purpose: the binary then derives "dev+<commit>[-dirty]" from the
# VCS stamp the toolchain embeds. Releases are versioned by GoReleaser; override
# manually with `make build VERSION=v1.2.3`.
VERSION  ?=
LDFLAGS  := -s -w $(if $(VERSION),-X $(MODULE)/internal/version.Version=$(VERSION))

# Extra flags for `go test` (e.g. make test TESTFLAGS="-run TestFoo -v").
TESTFLAGS ?=
# Arguments passed to the binary by `make run`.
ARGS ?=

# Pinned tool versions. Local binaries on PATH win; otherwise `go run` fetches
# the pinned version, so a bare checkout needs nothing but the Go toolchain.
GOLANGCI_VERSION   ?= v2.12.2
MODERNIZE_VERSION  ?= v0.23.0
GORELEASER_VERSION ?= v2.13.3

GOLANGCI   := $(shell command -v golangci-lint 2>/dev/null || echo "$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)")
GORELEASER := $(shell command -v goreleaser 2>/dev/null || echo "$(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)")
MODERNIZE  := $(GO) run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@$(MODERNIZE_VERSION)

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## --- build ---------------------------------------------------------------

.PHONY: build
build: ## Build ./jw for the host platform
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) $(MAIN_PKG)

.PHONY: build-all
build-all: ## Compile every package (no binary output)
	$(GO) build ./...

.PHONY: install
install: ## Install jw into $$GOBIN
	CGO_ENABLED=0 $(GO) install -trimpath -ldflags '$(LDFLAGS)' $(MAIN_PKG)

.PHONY: run
run: ## Run the CLI: make run ARGS="search jehovah"
	$(GO) run -ldflags '$(LDFLAGS)' $(MAIN_PKG) $(ARGS)

## --- test ----------------------------------------------------------------

.PHONY: test
test: ## Run tests
	$(GO) test ./... $(TESTFLAGS)

.PHONY: test-race
test-race: ## Run tests with the race detector
	$(GO) test -race ./... $(TESTFLAGS)

.PHONY: cover
cover: ## Run tests, write coverage.out + coverage.html
	$(GO) test -coverprofile=$(COVER_FILE) -covermode=atomic ./... $(TESTFLAGS)
	$(GO) tool cover -html=$(COVER_FILE) -o $(COVER_HTML)
	@$(GO) tool cover -func=$(COVER_FILE) | tail -1

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -run '^$$' -bench . -benchmem ./... $(TESTFLAGS)

## --- lint ----------------------------------------------------------------

.PHONY: fmt
fmt: ## Format code (gofmt + goimports)
	$(GOLANGCI) fmt

.PHONY: fmt-check
fmt-check: ## Fail if code is not formatted
	$(GOLANGCI) fmt --diff

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (includes modernize)
	$(GOLANGCI) run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with --fix
	$(GOLANGCI) run --fix

.PHONY: modernize
modernize: ## Report modern-Go simplifications
	$(MODERNIZE) ./...

.PHONY: modernize-fix
modernize-fix: ## Apply modern-Go simplifications in place
	$(MODERNIZE) -fix ./...

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod / go.sum are not tidy
	$(GO) mod tidy -diff

.PHONY: check
check: fmt-check vet lint test-race ## Everything CI runs

## --- release -------------------------------------------------------------

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	$(GORELEASER) check

.PHONY: snapshot
snapshot: ## Build all release artifacts locally into dist/
	$(GORELEASER) release --snapshot --clean

## --- housekeeping --------------------------------------------------------

.PHONY: tools
tools: ## Install pinned dev tools into $$GOBIN
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

.PHONY: clean
clean: ## Remove build and test output
	rm -rf $(BINARY) $(DIST_DIR) $(COVER_FILE) $(COVER_HTML)
	$(GO) clean -testcache
