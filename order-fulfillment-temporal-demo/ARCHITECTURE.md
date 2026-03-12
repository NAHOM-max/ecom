# Architecture Documentation

## Overview

This project demonstrates a production-grade distributed order fulfillment system using Temporal workflows and Clean Architecture principles.

## Clean Architecture Layers

### 1. Domain Layer (`internal/domain/`)
- **Pure business logic** with no external dependencies
- Contains entities, value objects, and domain services
- Defines repository interfaces (ports)
- No knowledge of Temporal, HTTP, or databases

**Files:**
- `order/entity.go` - Order entity with business rules
- `order/repository.go` - Repository interface (port)
- `order/service.go` - Domain service for complex operations

### 2. Application Layer (`internal/application/`)
- **Use cases and orchestration**
- Temporal workflows and activities
- Signal and query definitions
- Coordinates domain objects

**Files:**
- `workflows/order_workflow.go` - Main order fulfillment workflow
- `workflows/shipment_workflow.go` - Child workflow for shipping
- `activities/inventory_activity.go` - Inventory operations
- `activities/payment_activity.go` - Payment processing
- `activities/shipping_activity.go` - Shipping operations
- `signals/order_signals.go` - Signal definitions
- `queries/order_queries.go` - Query definitions

### 3. Infrastructure Layer (`internal/infrastructure/`)
- **External integrations and implementations**
- Database repositories (implements domain interfaces)
- Temporal client and worker wrappers
- Third-party service clients

**Files:**
- `repositories/order_repository.go` - Order repository implementation
- `temporal/client.go` - Temporal client wrapper
- `temporal/worker.go` - Temporal worker wrapper

### 4. Interface Layer (`internal/interfaces/`)
- **External communication**
- HTTP handlers and routes
- Request/response DTOs
- API documentation

**Files:**
- `http/order_handler.go` - HTTP request handlers
- `http/router.go` - Route configuration

### 5. Platform Layer (`platform/`)
- **Shared utilities**
- Logger, config, metrics
- Cross-cutting concerns

**Files:**
- `logger/logger.go` - Structured logging
- `config/config.go` - Configuration management

## Temporal Patterns Demonstrated

### 1. Durable Orchestration
The OrderWorkflow orchestrates multiple steps with automatic state persistence.

### 2. Saga Pattern (Compensation)
If payment fails, inventory is automatically released through compensation logic.

### 3. Child Workflows
ShipmentWorkflow runs as a child workflow with independent lifecycle.

### 4. Signals
External events (cancellation, updates) are sent to running workflows.

### 5. Queries
Workflow state can be queried without affecting execution.

### 6. Activities with Retries
Each activity has retry policies for transient failures.

### 7. Idempotency
Activities are designed to be safely retried.

## Workflow Execution Flow

```
1. API receives order request
2. Start OrderWorkflow
3. Reserve inventory (activity)
4. Process payment (activity)
   - On failure: Release inventory (compensation)
5. Start ShipmentWorkflow (child workflow)
   - Create shipment
   - Assign carrier
   - Generate label
   - Schedule pickup
6. Complete order
7. Return result
```

## Dependency Flow

```
Interfaces → Application → Domain
     ↓
Infrastructure
```

- Interfaces depend on Application
- Application depends on Domain
- Infrastructure implements Domain interfaces
- Domain has NO dependencies

## Testing Strategy

- **Unit tests**: Domain logic (entity_test.go)
- **Workflow tests**: Temporal test framework (order_workflow_test.go)
- **Activity tests**: Mock external services (inventory_activity_test.go)
- **Integration tests**: End-to-end with test Temporal server

## Configuration

Configuration is loaded from:
1. `config.yaml` file
2. Environment variables (override file)
3. Default values

## Running the System

1. Start dependencies: `make docker-up`
2. Run worker: `make run-worker`
3. Run API: `make run-api`
4. Access Temporal UI: http://localhost:8080

## Key Design Decisions

1. **Workflows don't depend on infrastructure** - ensures testability
2. **Activities in application layer** - they're use cases, not infrastructure
3. **Domain defines interfaces** - Dependency Inversion Principle
4. **Separate API and worker** - allows independent scaling
5. **Structured logging** - observability and debugging
