# Temporal Signals Implementation - OrderWorkflow

## Overview

The OrderWorkflow now supports **Temporal Signals** for external communication with running workflows. Signals allow external systems to send messages to workflows without restarting them, enabling dynamic behavior while maintaining determinism.

---

## Implemented Signals

### 1. cancel_order
**Purpose:** Triggers workflow cancellation and saga compensation

**Signal Name:** `cancel-order`

**Payload:**
```go
type CancelOrderRequest struct {
    Reason    string  // Cancellation reason
    RequestBy string  // Who requested (customer ID, admin ID, system)
    Timestamp int64   // Unix timestamp
}
```

**Behavior:**
- Immediately stops workflow progression
- Triggers saga compensation based on completed steps
- Returns workflow with status "CANCELLED"

**Compensation Logic:**
- If cancelled before inventory: No compensation needed
- If cancelled after inventory: Release inventory
- If cancelled after payment: Refund payment + Release inventory
- If cancelled after shipment: Full compensation (not recommended)

---

### 2. update_shipping_address
**Purpose:** Updates shipping address without restarting workflow

**Signal Name:** `update-shipping-address`

**Payload:**
```go
type UpdateShippingAddressRequest struct {
    Name       string
    Street     string
    City       string
    State      string
    PostalCode string
    Country    string
    Phone      string
    UpdatedBy  string  // Who updated the address
    Timestamp  int64   // Unix timestamp
}
```

**Behavior:**
- Updates workflow state with new address
- Address is used when creating shipment
- Does not interrupt workflow execution
- Safe to call multiple times (last update wins)

---

## Implementation Details

### Signal Channels Setup

```go
// Setup signal channels at workflow start
cancelChannel := workflow.GetSignalChannel(ctx, signals.CancelOrderSignal)
updateAddressChannel := workflow.GetSignalChannel(ctx, signals.UpdateShippingAddressSignal)
```

### Using workflow.Selector for Deterministic Signal Handling

The implementation uses `workflow.Selector` to listen for signals while activities execute:

```go
func executeActivityWithSignals(
    ctx workflow.Context,
    cancelChannel workflow.ReceiveChannel,
    updateAddressChannel workflow.ReceiveChannel,
    state *OrderWorkflowState,
    logger log.Logger,
    activityFunc func() error,
) error {
    // Create selector for multiplexing
    selector := workflow.NewSelector(ctx)
    activityDone := false
    var activityErr error

    // Start activity in deterministic goroutine
    activityFuture := workflow.Go(ctx, func(ctx workflow.Context) {
        activityErr = activityFunc()
        activityDone = true
    })

    // Add activity completion to selector
    selector.AddFuture(activityFuture, func(f workflow.Future) {
        // Activity completed
    })

    // Add cancel signal to selector
    selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
        var cancelRequest signals.CancelOrderRequest
        c.Receive(ctx, &cancelRequest)
        state.CancelRequested = true
        state.CancelReason = fmt.Sprintf("Order cancelled: %s (by %s)", 
            cancelRequest.Reason, cancelRequest.RequestBy)
        logger.Warn("Cancel signal received during activity")
    })

    // Add update address signal to selector
    selector.AddReceive(updateAddressChannel, func(c workflow.ReceiveChannel, more bool) {
        var updateRequest signals.UpdateShippingAddressRequest
        c.Receive(ctx, &updateRequest)
        state.ShippingAddress = &ShippingAddress{
            Name:       updateRequest.Name,
            Street:     updateRequest.Street,
            City:       updateRequest.City,
            State:      updateRequest.State,
            PostalCode: updateRequest.PostalCode,
            Country:    updateRequest.Country,
            Phone:      updateRequest.Phone,
        }
        logger.Info("Shipping address updated")
    })

    // Wait for activity while listening for signals
    for !activityDone {
        selector.Select(ctx)  // Blocks until signal or activity completes
    }

    return activityErr
}
```

---

## Determinism Guarantees

### Why workflow.Selector?

1. **Deterministic Execution:** `workflow.Selector` ensures signals are processed in a deterministic order during replay
2. **Non-blocking Checks:** Can check for signals without blocking workflow execution
3. **Multiplexing:** Listen to multiple channels (signals + activity completion) simultaneously
4. **Replay Safety:** Signal handling is recorded in workflow history

### Determinism Rules Followed

✅ **Use workflow.GetSignalChannel()** - Not regular Go channels  
✅ **Use workflow.Selector** - For multiplexing signals and futures  
✅ **Use workflow.Go()** - For deterministic goroutines  
✅ **Use workflow.Now()** - For timestamps (not time.Now())  
✅ **State updates are persisted** - Survives worker crashes  
✅ **No side effects in signal handlers** - Only state updates  

---

## Signal Flow Diagrams

### Cancel Signal Flow

```
External System                 OrderWorkflow                    Activities
      │                               │                               │
      │  1. Send cancel-order signal  │                               │
      ├──────────────────────────────>│                               │
      │                               │                               │
      │                               │  2. Selector receives signal  │
      │                               │     (non-blocking)            │
      │                               │                               │
      │                               │  3. Set CancelRequested=true  │
      │                               │                               │
      │                               │  4. Wait for activity to      │
      │                               │     complete (if running)     │
      │                               │<──────────────────────────────│
      │                               │                               │
      │                               │  5. Check CancelRequested     │
      │                               │                               │
      │                               │  6. Trigger compensation      │
      │                               │     - Refund payment          │
      │                               │     - Release inventory       │
      │                               │                               │
      │                               │  7. Return CANCELLED status   │
      │<──────────────────────────────│                               │
```

### Update Address Signal Flow

```
External System                 OrderWorkflow                    ShipmentWorkflow
      │                               │                               │
      │  1. Send update-address       │                               │
      ├──────────────────────────────>│                               │
      │                               │                               │
      │                               │  2. Selector receives signal  │
      │                               │                               │
      │                               │  3. Update state.ShippingAddress
      │                               │                               │
      │                               │  4. Continue processing       │
      │                               │                               │
      │                               │  5. Start ShipmentWorkflow    │
      │                               │     with updated address      │
      │                               ├──────────────────────────────>│
      │                               │                               │
      │                               │  6. Shipment uses new address │
      │                               │<──────────────────────────────│
      │                               │                               │
      │                               │  7. Complete successfully     │
      │<──────────────────────────────│                               │
```

---

## Cancellation Scenarios

### Scenario 1: Cancel Before Inventory
```
Timeline: Start → [CANCEL] → Inventory → Payment → Shipment → Complete

Result:
- Status: CANCELLED
- Compensation: None needed
- Message: "Order cancelled: {reason} (by {requestBy})"
```

### Scenario 2: Cancel After Inventory
```
Timeline: Start → Inventory ✅ → [CANCEL] → Payment → Shipment → Complete

Result:
- Status: CANCELLED
- Compensation: Release inventory (res-123)
- Message: "Order cancelled: {reason} (by {requestBy})"
```

### Scenario 3: Cancel After Payment
```
Timeline: Start → Inventory ✅ → Payment ✅ → [CANCEL] → Shipment → Complete

Result:
- Status: CANCELLED
- Compensation: 
  1. Refund payment (pay-456)
  2. Release inventory (res-123)
- Message: "Order cancelled: {reason} (by {requestBy})"
```

### Scenario 4: Cancel Too Late (After Completion)
```
Timeline: Start → Inventory ✅ → Payment ✅ → Shipment ✅ → Complete ✅ → [CANCEL]

Result:
- Status: COMPLETED (signal ignored)
- Compensation: None (workflow already completed)
- Note: Signal is queued but workflow has finished
```

---

## Address Update Scenarios

### Scenario 1: Update Before Shipment
```
Timeline: Start → Inventory → Payment → [UPDATE ADDRESS] → Shipment → Complete

Result:
- Shipment uses updated address
- No workflow restart needed
- State persisted with new address
```

### Scenario 2: Multiple Updates
```
Timeline: Start → [UPDATE 1] → Inventory → [UPDATE 2] → Payment → Shipment → Complete

Result:
- Last update wins (UPDATE 2)
- All updates logged
- Shipment uses final address
```

### Scenario 3: Update After Shipment Started
```
Timeline: Start → Inventory → Payment → Shipment Started → [UPDATE ADDRESS] → Complete

Result:
- Update recorded in state
- Shipment already started with old address
- Update may be too late (depends on timing)
```

---

## Sending Signals (Client Side)

### Using Temporal Go SDK

```go
import (
    "go.temporal.io/sdk/client"
    "github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
)

// Connect to Temporal
c, err := client.Dial(client.Options{
    HostPort: "localhost:7233",
})
if err != nil {
    log.Fatal(err)
}
defer c.Close()

// Send cancel signal
err = c.SignalWorkflow(
    context.Background(),
    "order-123",  // Workflow ID
    "",           // Run ID (empty = current run)
    signals.CancelOrderSignal,
    signals.CancelOrderRequest{
        Reason:    "Customer requested cancellation",
        RequestBy: "customer-456",
        Timestamp: time.Now().Unix(),
    },
)

// Send update address signal
err = c.SignalWorkflow(
    context.Background(),
    "order-123",
    "",
    signals.UpdateShippingAddressSignal,
    signals.UpdateShippingAddressRequest{
        Name:       "Jane Doe",
        Street:     "789 New St",
        City:       "Los Angeles",
        State:      "CA",
        PostalCode: "90001",
        Country:    "USA",
        Phone:      "555-9999",
        UpdatedBy:  "customer-456",
        Timestamp:  time.Now().Unix(),
    },
)
```

### Using Temporal CLI

```bash
# Cancel order
temporal workflow signal \
    --workflow-id order-123 \
    --name cancel-order \
    --input '{"Reason":"Customer cancelled","RequestBy":"customer-456","Timestamp":1234567890}'

# Update shipping address
temporal workflow signal \
    --workflow-id order-123 \
    --name update-shipping-address \
    --input '{"Name":"Jane Doe","Street":"789 New St","City":"Los Angeles","State":"CA","PostalCode":"90001","Country":"USA","Phone":"555-9999","UpdatedBy":"customer-456","Timestamp":1234567890}'
```

### Using HTTP API (via REST endpoint)

```bash
# Cancel order
curl -X POST http://localhost:8080/api/v1/orders/order-123/cancel \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "Customer requested cancellation",
    "requestBy": "customer-456"
  }'

# Update shipping address
curl -X PATCH http://localhost:8080/api/v1/orders/order-123/address \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Jane Doe",
    "street": "789 New St",
    "city": "Los Angeles",
    "state": "CA",
    "postalCode": "90001",
    "country": "USA",
    "phone": "555-9999"
  }'
```

---

## Testing

### Test Coverage

✅ **TestOrderWorkflow_CancelSignal_BeforeInventory**
- Cancel before any activities
- No compensation needed

✅ **TestOrderWorkflow_CancelSignal_AfterInventory**
- Cancel after inventory reserved
- Compensates inventory only

✅ **TestOrderWorkflow_CancelSignal_AfterPayment**
- Cancel after payment charged
- Compensates payment + inventory

✅ **TestOrderWorkflow_UpdateShippingAddress**
- Update address during processing
- Shipment uses updated address

✅ **TestOrderWorkflow_MultipleSignals**
- Multiple address updates
- Final cancel signal
- All signals handled correctly

✅ **TestOrderWorkflow_CancelSignal_TooLate**
- Cancel after workflow completes
- Signal ignored, order completed

### Running Tests

```bash
# Run all signal tests
go test ./internal/application/workflows -v -run Signal

# Run specific test
go test ./internal/application/workflows -v -run TestOrderWorkflow_CancelSignal_AfterPayment

# Run with coverage
go test ./internal/application/workflows -v -cover -run Signal
```

---

## State Management

### OrderWorkflowState with Signals

```go
type OrderWorkflowState struct {
    OrderID           string
    Status            string
    InventoryReserved bool
    ReservationID     string
    PaymentCharged    bool
    PaymentID         string
    ShipmentCreated   bool
    ShipmentID        string
    ShippingAddress   *ShippingAddress  // ⬅️ Updated by signal
    CancelRequested   bool              // ⬅️ Set by cancel signal
    CancelReason      string            // ⬅️ Cancellation details
    CompletedSteps    []string
    LastUpdated       time.Time
}
```

### State Persistence

- State is automatically persisted by Temporal
- Survives worker crashes and restarts
- Signal updates are part of workflow history
- Replay-safe and deterministic

---

## Best Practices

### ✅ DO

1. **Use workflow.Selector** for signal handling
2. **Check signals between activities** (not during)
3. **Update state, not external systems** in signal handlers
4. **Log all signal events** for observability
5. **Validate signal payloads** before processing
6. **Use typed signal structs** for type safety
7. **Handle multiple signals** gracefully

### ❌ DON'T

1. **Don't call activities** in signal handlers
2. **Don't use time.Now()** - use workflow.Now()
3. **Don't use regular Go channels** - use workflow channels
4. **Don't block indefinitely** waiting for signals
5. **Don't ignore signals** - always handle or log
6. **Don't modify external state** in signal handlers
7. **Don't assume signal order** (use timestamps)

---

## Production Considerations

### Monitoring

- Track signal delivery rates
- Monitor cancellation reasons
- Alert on high cancellation rates
- Track address update frequency

### Observability

```go
logger.Info("Signal received",
    "signalName", signals.CancelOrderSignal,
    "orderID", state.OrderID,
    "reason", cancelRequest.Reason,
    "requestBy", cancelRequest.RequestBy,
    "timestamp", cancelRequest.Timestamp,
    "currentStatus", state.Status)
```

### Error Handling

- Signals are queued if workflow is busy
- Signals are durable (survive crashes)
- Invalid signals are logged but don't fail workflow
- Signal handlers should be idempotent

### Security

- Validate `RequestBy` field (authentication)
- Check authorization before processing
- Audit all cancellations
- Rate limit signal sending

---

## Summary

### Key Features

✅ **cancel_order signal** - Triggers cancellation with compensation  
✅ **update_shipping_address signal** - Updates address without restart  
✅ **workflow.Selector** - Deterministic signal multiplexing  
✅ **Saga compensation** - Automatic rollback on cancellation  
✅ **State persistence** - Survives crashes  
✅ **Comprehensive tests** - All scenarios covered  
✅ **Production-ready** - Logging, monitoring, error handling  

### Determinism Maintained

✅ Uses workflow.GetSignalChannel()  
✅ Uses workflow.Selector for multiplexing  
✅ Uses workflow.Go() for goroutines  
✅ Uses workflow.Now() for timestamps  
✅ No side effects in signal handlers  
✅ State updates only  
✅ Replay-safe  

**Status: FULLY IMPLEMENTED AND TESTED! 🚀**
