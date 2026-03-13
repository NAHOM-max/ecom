# ShipmentWorkflow - Visual Flow Diagram

## Complete Order Fulfillment Flow with Child Workflow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         OrderWorkflow (Parent)                          │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 1: Reserve Inventory                        │
        │  ├─ Call: ReserveInventory Activity               │
        │  ├─ Retry: 5 attempts, 2s → 4s → 8s → 16s        │
        │  └─ Result: reservationID = "res-abc"             │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 2: Charge Payment                           │
        │  ├─ Call: ChargePayment Activity                  │
        │  ├─ Retry: 5 attempts, exponential backoff        │
        │  └─ Result: paymentID = "pay-xyz"                 │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 3: Create Shipment (Child Workflow)         │
        │  ├─ WorkflowID: "order-123-shipment"              │
        │  ├─ Retry: 3 attempts at workflow level           │
        │  └─ Parent WAITS for child result ⏳              │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌─────────────────────────────────────────────────────────────┐
        │              ShipmentWorkflow (Child)                       │
        ├─────────────────────────────────────────────────────────────┤
        │                                                             │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │ Step 1: Create Shipment                             │   │
        │  │ ├─ Call: CreateShipment Activity                    │   │
        │  │ ├─ Validate: Address, items, shipping method        │   │
        │  │ ├─ Generate: shipmentID, trackingNumber, carrier    │   │
        │  │ ├─ Retry: 5 attempts, 2s → 4s → 8s → 16s           │   │
        │  │ └─ Result: shipmentID = "ship-456"                  │   │
        │  └─────────────────────────────────────────────────────┘   │
        │                          │                                  │
        │                          ▼                                  │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │ Step 2: Wait for Shipping Confirmation              │   │
        │  │ ├─ Simulate: Carrier confirmation delay             │   │
        │  │ ├─ Duration: 3 seconds (deterministic)              │   │
        │  │ ├─ Method: workflow.Sleep(3s)                       │   │
        │  │ └─ Handles: Cancellation gracefully                 │   │
        │  └─────────────────────────────────────────────────────┘   │
        │                          │                                  │
        │                          ▼                                  │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │ Step 3: Mark Shipment Completed                     │   │
        │  │ ├─ Update: Status = "COMPLETED"                     │   │
        │  │ ├─ Log: All completed steps                         │   │
        │  │ └─ Return: ShipmentWorkflowResult                   │   │
        │  └─────────────────────────────────────────────────────┘   │
        │                                                             │
        └─────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 4: Complete Order                           │
        │  ├─ Status: "COMPLETED"                           │
        │  ├─ PaymentID: "pay-xyz"                          │
        │  ├─ ShipmentID: "ship-456"                        │
        │  └─ TrackingNumber: "1Z999AA10123456784"          │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
                            ✅ SUCCESS


═══════════════════════════════════════════════════════════════════════════


## Failure Scenario with Saga Compensation

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         OrderWorkflow (Parent)                          │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 1: Reserve Inventory                        │
        │  └─ Result: reservationID = "res-abc" ✅          │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 2: Charge Payment                           │
        │  └─ Result: paymentID = "pay-xyz" ✅              │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  Step 3: Create Shipment (Child Workflow)         │
        │  └─ Parent WAITS for child result ⏳              │
        └───────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌─────────────────────────────────────────────────────────────┐
        │              ShipmentWorkflow (Child)                       │
        ├─────────────────────────────────────────────────────────────┤
        │                                                             │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │ Step 1: Create Shipment                             │   │
        │  │ ├─ Call: CreateShipment Activity                    │   │
        │  │ ├─ Error: Invalid shipping address ❌               │   │
        │  │ └─ Type: Business Error (non-retryable)             │   │
        │  └─────────────────────────────────────────────────────┘   │
        │                          │                                  │
        │                          ▼                                  │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │ Return Error to Parent                              │   │
        │  │ └─ Success: false, Message: "Invalid address"       │   │
        │  └─────────────────────────────────────────────────────┘   │
        │                                                             │
        └─────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │  ❌ Shipment Failed - Trigger Compensation        │
        └───────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
    ┌───────────────────────────┐   ┌───────────────────────────┐
    │ Compensation 1:           │   │ Compensation 2:           │
    │ Refund Payment            │   │ Release Inventory         │
    │                           │   │                           │
    │ ├─ Call: RefundPayment    │   │ ├─ Call: ReleaseInventory │
    │ ├─ Input: "pay-xyz"       │   │ ├─ Input: "res-abc"       │
    │ ├─ Retry: 5 attempts      │   │ ├─ Retry: 5 attempts      │
    │ └─ Result: Refunded ✅    │   │ └─ Result: Released ✅    │
    └───────────────────────────┘   └───────────────────────────┘
                    │                               │
                    └───────────────┬───────────────┘
                                    ▼
                    ┌───────────────────────────────┐
                    │  Order Status: FAILED         │
                    │  Compensation: COMPLETE       │
                    │  Message: "Invalid address"   │
                    └───────────────────────────────┘
                                    │
                                    ▼
                            ❌ FAILED (Compensated)


═══════════════════════════════════════════════════════════════════════════


## Retry Logic Visualization

### Activity Retry (5 attempts)
```
Attempt 1: ──────────────────────────────────────────────────────────> ❌
           (immediate)

Attempt 2: ──[2s delay]──────────────────────────────────────────────> ❌
           
Attempt 3: ──[4s delay]──────────────────────────────────────────────> ❌
           
Attempt 4: ──[8s delay]──────────────────────────────────────────────> ❌
           
Attempt 5: ──[16s delay]─────────────────────────────────────────────> ❌
           
Result: All attempts failed → Trigger Compensation
```

### Child Workflow Retry (3 attempts)
```
Attempt 1: ShipmentWorkflow ──────────────────────────────────────────> ❌
           (immediate)

Attempt 2: ShipmentWorkflow ──[2s delay]──────────────────────────────> ❌
           
Attempt 3: ShipmentWorkflow ──[4s delay]──────────────────────────────> ❌
           
Result: All attempts failed → Return error to parent → Compensation
```


═══════════════════════════════════════════════════════════════════════════


## State Persistence Timeline

```
Time    OrderWorkflow State                    ShipmentWorkflow State
────────────────────────────────────────────────────────────────────────────
T0      Status: PROCESSING                     (not started)
        InventoryReserved: false
        PaymentCharged: false
        ShipmentCreated: false

T1      Status: RESERVING_INVENTORY            (not started)
        InventoryReserved: false
        ↓ (activity executing)

T2      Status: RESERVING_INVENTORY            (not started)
        InventoryReserved: true ✅
        ReservationID: "res-abc"

T3      Status: CHARGING_PAYMENT               (not started)
        PaymentCharged: false
        ↓ (activity executing)

T4      Status: CHARGING_PAYMENT               (not started)
        PaymentCharged: true ✅
        PaymentID: "pay-xyz"

T5      Status: CREATING_SHIPMENT              Status: CREATING
        ShipmentCreated: false                 ShipmentCreated: false
        ↓ (child workflow started)             ↓ (activity executing)

T6      Status: CREATING_SHIPMENT              Status: CREATING_SHIPMENT
        (waiting for child)                    ShipmentCreated: false
                                               ↓ (activity executing)

T7      Status: CREATING_SHIPMENT              Status: CREATING_SHIPMENT
        (waiting for child)                    ShipmentCreated: true ✅
                                               ShipmentID: "ship-456"

T8      Status: CREATING_SHIPMENT              Status: AWAITING_CONFIRMATION
        (waiting for child)                    ConfirmationSent: false
                                               ↓ (sleeping 3s)

T9      Status: CREATING_SHIPMENT              Status: AWAITING_CONFIRMATION
        (waiting for child)                    ConfirmationSent: true ✅

T10     Status: CREATING_SHIPMENT              Status: COMPLETED ✅
        (waiting for child)                    CompletedSteps: [created, confirmed, completed]

T11     Status: CREATING_SHIPMENT              (child returned)
        ShipmentCreated: true ✅
        ShipmentID: "ship-456"

T12     Status: COMPLETED ✅                   (child completed)
        CompletedSteps: [inventory, payment, shipment, completed]
```


═══════════════════════════════════════════════════════════════════════════


## Timeout Handling

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Activity Timeout (5 minutes)                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  CreateShipment Activity                                            │
│  ├─ Start: T0                                                       │
│  ├─ Timeout: T0 + 5min                                              │
│  │                                                                   │
│  ├─ If completes before timeout: ✅ Success                         │
│  └─ If exceeds timeout: ❌ TimeoutError → Retry                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                 Compensation Timeout (3 minutes)                    │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  RefundPayment Activity                                             │
│  ├─ Start: T0                                                       │
│  ├─ Timeout: T0 + 3min                                              │
│  │                                                                   │
│  ├─ If completes before timeout: ✅ Refunded                        │
│  └─ If exceeds timeout: ❌ TimeoutError → Retry (5 attempts)        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```


═══════════════════════════════════════════════════════════════════════════


## Key Implementation Details

### 1. Child Workflow Registration
```go
// In worker.go
worker.RegisterWorkflow(workflows.OrderWorkflow)      // Parent
worker.RegisterWorkflow(workflows.ShipmentWorkflow)   // Child ⬅️ MUST register
```

### 2. Parent Invokes Child
```go
// In order_workflow.go:189-210
childWorkflowOptions := workflow.ChildWorkflowOptions{
    WorkflowID: input.OrderID + "-shipment",  // Unique ID
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    time.Second * 2,
        BackoffCoefficient: 2.0,
        MaximumAttempts:    3,
    },
}

childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

var shipmentResult ShipmentWorkflowResult
err = workflow.ExecuteChildWorkflow(
    childCtx, 
    ShipmentWorkflow,      // Child workflow function
    ShipmentWorkflowInput{...}
).Get(ctx, &shipmentResult)  // ⬅️ BLOCKS until child completes
```

### 3. Child Returns Result
```go
// In shipment_workflow.go:195-201
return &ShipmentWorkflowResult{
    ShipmentID:     state.ShipmentID,
    TrackingNumber: state.TrackingNumber,
    Carrier:        state.Carrier,
    EstimatedDate:  createResult.EstimatedDate,
    Success:        true,
    Message:        "Shipment completed successfully",
}, nil  // ⬅️ Returns to parent
```

### 4. Parent Handles Result
```go
// In order_workflow.go:212-227
if err != nil {
    // Technical error → Compensate
    compensatePayment(ctx, logger, state.PaymentID)
    compensateInventory(ctx, logger, state.ReservationID)
    return &OrderWorkflowResult{Status: "FAILED", ...}, err
}

if !shipmentResult.Success {
    // Business error → Compensate
    compensatePayment(ctx, logger, state.PaymentID)
    compensateInventory(ctx, logger, state.ReservationID)
    return &OrderWorkflowResult{Status: "FAILED", ...}, nil
}

// Success → Continue
state.ShipmentID = shipmentResult.ShipmentID
```


═══════════════════════════════════════════════════════════════════════════


## Summary

✅ **Child Workflow:** ShipmentWorkflow is a separate workflow
✅ **Lifecycle:** Create → Confirm → Complete (3 steps)
✅ **Retry Logic:** 5 attempts for activities, 3 for child workflow
✅ **Timeout Handling:** 5 min activity timeout, 3 min compensation timeout
✅ **Parent Waits:** Blocks on Get() until child completes
✅ **Saga Compensation:** Automatic rollback on failure
✅ **State Persistence:** Both parent and child maintain state
✅ **Production Ready:** Logging, monitoring, error handling

**Status: FULLY IMPLEMENTED AND TESTED! 🚀**
```
