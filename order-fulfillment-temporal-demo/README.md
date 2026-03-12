# Order Fulfillment Temporal Demo

A production-style distributed order fulfillment backend demonstrating Temporal workflows with Clean Architecture.

## Features

- Durable orchestration with Temporal
- Automatic retries and error handling
- Saga compensation patterns
- Child workflows for complex operations
- Signals for external events
- Updates for workflow state modifications
- Queries for workflow state inspection
- Distributed activities across services

## Architecture

Clean Architecture with clear separation:
- **Domain Layer**: Business entities and interfaces (no external dependencies)
- **Application Layer**: Workflows, activities, signals, queries
- **Infrastructure Layer**: Temporal client, repositories, external integrations
- **Interface Layer**: HTTP handlers, API routes

## Project Structure

```
cmd/                    # Application entry points
internal/               # Private application code
  domain/              # Business logic and entities
  application/         # Use cases (workflows & activities)
  infrastructure/      # External integrations
  interfaces/          # API handlers
platform/              # Shared utilities
docker/                # Container configurations
```

## Getting Started

1. Start Temporal server: `docker-compose up -d`
2. Run worker: `go run cmd/worker/main.go`
3. Run API: `go run cmd/api/main.go`
