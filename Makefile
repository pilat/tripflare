.PHONY: all build lint test test-coverage fmt clean ci help

APP_NAME := tripflare
TEST_FLAGS := -race -timeout=30s
COVERAGE_FILE := coverage.out

all: lint test

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build for current platform
	go build -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

lint: ## Run linter (includes govet)
	golangci-lint run ./...

test: ## Run tests
	go test $(TEST_FLAGS) ./...

test-coverage: ## Run tests with coverage
	go test $(TEST_FLAGS) -coverprofile=$(COVERAGE_FILE) ./...
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html

fmt: ## Format code (gofumpt + gci + golines via golangci-lint)
	golangci-lint fmt ./...

ci: fmt lint test ## Run full CI pipeline

clean: ## Clean build artifacts
	rm -rf bin/ $(COVERAGE_FILE) coverage.html
