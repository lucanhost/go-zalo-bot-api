GO ?= go
PKG := ./...
COVERAGE := coverage.out
GOLANGCI_LINT ?= golangci-lint

.PHONY: help fmt fmt-check vet test test-race cover lint tidy integration ci clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

fmt: ## Format all Go source files
	$(GO) fmt $(PKG)

fmt-check: ## Fail if any Go source file is not gofmt-clean
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

vet: ## Run go vet
	$(GO) vet $(PKG)

test: ## Run the hermetic test suite
	$(GO) test $(PKG)

test-race: ## Run the hermetic test suite with the race detector
	$(GO) test -race $(PKG)

cover: ## Run tests with coverage and print the total
	$(GO) test -race -covermode=atomic -coverprofile=$(COVERAGE) $(PKG)
	$(GO) tool cover -func=$(COVERAGE) | tail -1

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run

tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

integration: ## Run the live integration suite (requires ZALO_BOT_TOKEN)
	$(GO) test -tags integration -run TestLive -v $(PKG)

ci: fmt-check vet test-race lint ## Run everything CI runs

clean: ## Remove build artifacts
	rm -f $(COVERAGE)
