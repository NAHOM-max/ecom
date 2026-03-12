.PHONY: help build run-api run-worker test docker-up docker-down clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build all binaries
	@echo "Building API server..."
	go build -o bin/api cmd/api/main.go
	@echo "Building worker..."
	go build -o bin/worker cmd/worker/main.go

run-api: ## Run API server
	go run cmd/api/main.go

run-worker: ## Run Temporal worker
	go run cmd/worker/main.go

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests with coverage report
	go tool cover -html=coverage.out

docker-up: ## Start Temporal and dependencies
	docker-compose -f docker/docker-compose.yml up -d

docker-down: ## Stop Temporal and dependencies
	docker-compose -f docker/docker-compose.yml down

docker-logs: ## View docker logs
	docker-compose -f docker/docker-compose.yml logs -f

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out

deps: ## Download dependencies
	go mod download
	go mod tidy

fmt: ## Format code
	go fmt ./...

lint: ## Run linter
	golangci-lint run

.DEFAULT_GOAL := help
