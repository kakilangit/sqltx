GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2

.PHONY: all tools check test lint fmt fmt-check tidy-check benchmark clean

all: check test

tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

check: fmt-check tidy-check lint

test:
	$(GO) test -race -coverprofile=coverage.out ./...

lint:
	$(GOLANGCI_LINT) run ./...

fmt:
	$(GO) fmt ./...
	$(GOLANGCI_LINT) fmt ./...

fmt-check:
	@diff="$$($(GOLANGCI_LINT) fmt --diff ./...)"; \
		test -z "$$diff" || { printf '%s\n' "$$diff"; exit 1; }

tidy-check:
	$(GO) mod tidy -diff

benchmark:
	$(GO) test -run '^$$' -bench . -benchmem ./...

clean:
	rm -f coverage.out
