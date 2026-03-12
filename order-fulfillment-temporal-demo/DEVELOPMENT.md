# Development Guide

## Getting Started

### Initial Setup

1. **Verify Go installation:**
```bash
go version  # Should be 1.21+
```

2. **Navigate to project:**
```bash
cd order-fulfillment-temporal-demo
```

3. **Download dependencies:**
```bash
go mod download
go mod tidy
```

4. **Start Temporal:**
```bash
make docker-up
```

5. **Verify Temporal is running:**
- Open http://localhost:8080 (Temporal UI)
- Check namespace "default" exists

## Development Workflow

### Step 1: Implement Domain Layer (No Dependencies)

Start with pure business logic:

**File: internal/domain/order/entity.go**

```go
func (o *Order) Validate() error {
    if o.CustomerID == "" {
        return errors.New("customer ID is required")
    }
    if len(o.Items) == 0 {
        return errors.New("order must have at least one item")
    }
    for _, item := range o.Items {
        if item.Quantity <= 0 {
            return errors.New("item quantity must be positive")
        }
        if item.Price < 0 {
            return errors.New("item price cannot be negative")
        }
    }
    return nil
}

func (o *Order) CalculateTotal() float64 {
    total := 0.0
    for _, item := range o.Items {
        total += float64(item.Quantity) * item.Price
    }
    return total
}
```

**Test it:**
```bash
go test ./internal/domain/order/...
```

### Step 2: Implement Activities

Activities interact with external systems:

**File: internal/application/activities/inventory_activity.go**

```go
func (a *InventoryActivity) ReserveInventory(ctx context.Context, input ReserveInventoryInput) (*ReserveInventoryResult, error) {
    logger := activity.GetLogger(ctx)
    logger.Info("Reserving inventory", "orderID", input.OrderID)
    
    // Check for idempotency
    activityInfo := activity.GetInfo(ctx)
    reservationID := activityInfo.WorkflowExecution.ID + "-" + activityInfo.ActivityID
    
    // TODO: Check if already reserved
    // TODO: Check stock availability
    // TODO: Create reservation
    // TODO: Update inventory
    
    return &ReserveInventoryResult{
        ReservationID: reservationID,
        Success:       true,
        Message:       "Inventory reserved successfully",
    }, nil
}
```

### Step 3: Implement Workflows

Orchestrate activities:

**File: internal/application/workflows/order_workflow.go**

```go
func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
    logger := workflow.GetLogger(ctx)
    logger.Info("OrderWorkflow started", "orderID", input.OrderID)
    
    // Initialize state
    state := &OrderWorkflowState{
        Status:      "processing",
        LastUpdated: workflow.Now(ctx),
    }
    
    // Setup query handlers
    err := workflow.SetQueryHandler(ctx, "get-status", func() (string, error) {
        return state.Status, nil
    })
    if err != nil {
        return nil, err
    }
    
    // Setup signal handlers
    cancelChan := workflow.GetSignalChannel(ctx, "cancel-order")
    
    // Activity options with retry
    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: time.Minute * 5,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval:    time.Second,
            BackoffCoefficient: 2.0,
            MaximumInterval:    time.Minute,
            MaximumAttempts:    3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)
    
    // Step 1: Reserve Inventory
    var inventoryResult activities.ReserveInventoryResult
    err = workflow.ExecuteActivity(ctx, "ReserveInventory", activities.ReserveInventoryInput{
        OrderID: input.OrderID,
        Items:   convertItems(input.Items),
    }).Get(ctx, &inventoryResult)
    
    if err != nil {
        logger.Error("Failed to reserve inventory", "error", err)
        state.Status = "failed"
        return nil, err
    }
    
    state.InventoryHeld = true
    state.CompletedSteps = append(state.CompletedSteps, "inventory_reserved")
    
    // Step 2: Process Payment
    var paymentResult activities.ProcessPaymentResult
    err = workflow.ExecuteActivity(ctx, "ProcessPayment", activities.ProcessPaymentInput{
        OrderID:    input.OrderID,
        CustomerID: input.CustomerID,
        Amount:     calculateTotal(input.Items),
        Currency:   "USD",
    }).Get(ctx, &paymentResult)
    
    if err != nil {
        logger.Error("Payment failed, releasing inventory", "error", err)
        // Compensation: Release inventory
        _ = workflow.ExecuteActivity(ctx, "ReleaseInventory", inventoryResult.ReservationID).Get(ctx, nil)
        state.Status = "failed"
        return nil, err
    }
    
    state.PaymentID = paymentResult.PaymentID
    state.Status = "paid"
    state.CompletedSteps = append(state.CompletedSteps, "payment_processed")
    
    // Step 3: Create Shipment (Child Workflow)
    childWorkflowOptions := workflow.ChildWorkflowOptions{
        WorkflowID: input.OrderID + "-shipment",
    }
    childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)
    
    var shipmentResult workflows.ShipmentWorkflowResult
    err = workflow.ExecuteChildWorkflow(childCtx, "ShipmentWorkflow", workflows.ShipmentWorkflowInput{
        OrderID: input.OrderID,
        // ... other fields
    }).Get(ctx, &shipmentResult)
    
    if err != nil {
        logger.Error("Shipment failed", "error", err)
        // Compensation: Refund payment
        _ = workflow.ExecuteActivity(ctx, "RefundPayment", paymentResult.PaymentID).Get(ctx, nil)
        state.Status = "failed"
        return nil, err
    }
    
    state.ShipmentID = shipmentResult.ShipmentID
    state.Status = "shipped"
    state.CompletedSteps = append(state.CompletedSteps, "shipment_created")
    
    return &OrderWorkflowResult{
        OrderID:    input.OrderID,
        Status:     state.Status,
        PaymentID:  state.PaymentID,
        ShipmentID: state.ShipmentID,
    }, nil
}
```

### Step 4: Implement Worker

Register and start worker:

**File: cmd/worker/main.go**

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"
    
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
    
    "github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
    "github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
)

func main() {
    // Create Temporal client
    c, err := client.Dial(client.Options{
        HostPort: "localhost:7233",
    })
    if err != nil {
        log.Fatalln("Unable to create Temporal client", err)
    }
    defer c.Close()
    
    // Create worker
    w := worker.New(c, "order-fulfillment", worker.Options{})
    
    // Register workflows
    w.RegisterWorkflow(workflows.OrderWorkflow)
    w.RegisterWorkflow(workflows.ShipmentWorkflow)
    
    // Register activities
    inventoryActivity := activities.NewInventoryActivity()
    w.RegisterActivity(inventoryActivity.ReserveInventory)
    w.RegisterActivity(inventoryActivity.ReleaseInventory)
    
    paymentActivity := activities.NewPaymentActivity()
    w.RegisterActivity(paymentActivity.ProcessPayment)
    w.RegisterActivity(paymentActivity.RefundPayment)
    
    shippingActivity := activities.NewShippingActivity()
    w.RegisterActivity(shippingActivity.CreateShipment)
    w.RegisterActivity(shippingActivity.AssignCarrier)
    w.RegisterActivity(shippingActivity.GenerateLabel)
    
    // Start worker
    err = w.Start()
    if err != nil {
        log.Fatalln("Unable to start worker", err)
    }
    
    log.Println("Worker started successfully")
    
    // Wait for interrupt signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    <-sigCh
    
    log.Println("Shutting down worker...")
    w.Stop()
}
```

**Run the worker:**
```bash
go run cmd/worker/main.go
```

### Step 5: Implement API

**File: cmd/api/main.go**

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
    
    "github.com/gin-gonic/gin"
    "go.temporal.io/sdk/client"
    
    httpInterface "github.com/yourorg/order-fulfillment-temporal-demo/internal/interfaces/http"
)

func main() {
    // Create Temporal client
    c, err := client.Dial(client.Options{
        HostPort: "localhost:7233",
    })
    if err != nil {
        log.Fatalln("Unable to create Temporal client", err)
    }
    defer c.Close()
    
    // Create handlers
    orderHandler := httpInterface.NewOrderHandler(c)
    
    // Setup router
    router := gin.Default()
    router.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "healthy"})
    })
    
    v1 := router.Group("/api/v1")
    {
        orders := v1.Group("/orders")
        {
            orders.POST("", orderHandler.CreateOrder)
            orders.GET("/:id", orderHandler.GetOrder)
            orders.POST("/:id/cancel", orderHandler.CancelOrder)
        }
    }
    
    // Start server
    srv := &http.Server{
        Addr:    ":8080",
        Handler: router,
    }
    
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %s\n", err)
        }
    }()
    
    log.Println("API server started on :8080")
    
    // Wait for interrupt
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    
    log.Println("Shutting down server...")
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        log.Fatal("Server forced to shutdown:", err)
    }
    
    log.Println("Server exited")
}
```

**Run the API:**
```bash
go run cmd/api/main.go
```

### Step 6: Test End-to-End

**Create an order:**
```bash
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "order_id": "order-001",
    "customer_id": "cust-123",
    "items": [
      {
        "product_id": "prod-456",
        "quantity": 2,
        "price": 29.99
      }
    ]
  }'
```

**Check Temporal UI:**
- Go to http://localhost:8080
- Find your workflow execution
- View workflow history
- Check activity results

## Testing

### Unit Tests
```bash
# Test domain layer
go test ./internal/domain/...

# Test with coverage
go test -cover ./internal/domain/...
```

### Workflow Tests
```bash
go test ./internal/application/workflows/...
```

### Integration Tests
```bash
# Start test Temporal server
# Run integration tests
go test -tags=integration ./...
```

## Debugging

### View Logs
```bash
# Worker logs
go run cmd/worker/main.go

# API logs
go run cmd/api/main.go

# Docker logs
docker-compose -f docker/docker-compose.yml logs -f
```

### Temporal UI
- Workflow executions: http://localhost:8080/namespaces/default/workflows
- Task queues: http://localhost:8080/namespaces/default/task-queues
- Workflow history: Click on workflow ID

### Common Issues

**Worker not picking up tasks:**
- Check task queue name matches
- Verify workflows are registered
- Check Temporal connection

**Activities failing:**
- Check activity timeout settings
- Review retry policy
- Check external service availability

## Best Practices

1. **Always use activity options** - Set timeouts and retry policies
2. **Make activities idempotent** - Safe to retry
3. **Use workflow.GetLogger()** - For workflow logging
4. **Handle signals properly** - Use selectors for multiple signals
5. **Test workflows** - Use Temporal test framework
6. **Version workflows** - For backward compatibility
7. **Monitor task queues** - Prevent backlog

## Next Steps

1. Implement remaining activities
2. Add database integration
3. Add comprehensive tests
4. Add metrics and monitoring
5. Implement authentication
6. Add rate limiting
7. Deploy to staging environment

## Resources

- Temporal Docs: https://docs.temporal.io/
- Go SDK: https://pkg.go.dev/go.temporal.io/sdk
- Samples: https://github.com/temporalio/samples-go
