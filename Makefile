.PHONY: help test fmt lint clean deps

.DEFAULT_GOAL := help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

test: ## Run tests
	go test -v ./...

test-race: ## Run tests with race detection
	go test -v -race ./...

test-coverage: ## Run tests with coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

fmt: ## Format code
	gofmt -s -w .

lint: ## Run golangci-lint
	golangci-lint run

clean: ## Clean up generated files
	go clean
	rm -f coverage.out coverage.html

deps: ## Download dependencies  
	go mod tidy
	go mod download