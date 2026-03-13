# Temporal Signals - Quick Reference Guide

## Overview

The OrderWorkflow supports two signals for external communication:
- **cancel_order** - Cancel order with automatic compensation
- **update_shipping_address** - Update shipping address without restart

---

## Signal: cancel_order

### Purpose
Triggers immediate workflow cancellation with automatic saga compensation.

### When to Use
- Customer requests cancellation
- Fraud detection system flags order
- Payment authorization expires
- Inventory becomes unavailable
- Admin intervention required

### Payload
```json
{
  "Reason": "Customer requested cancellation",
  "RequestBy": "customer-456",
  "Timestamp": 1773392604
}
```

### Behavior by Stage

| Stage | Compensation Actions |
|-------|---------------------|
| Before Inventory | None |
| After Inventory | Release inventory |
| After Payment | Refund payment + Release inventory |
| After Shipment | Full compensation (not recommended) |
| After Completion | Signal ignored (too late) |

### Example: Send via Go SDK
```go
import (
    "context"
    "time"
    "go.temporal.io/sdk/client"
    "github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
)

func cancelOrder(workflowID string, reason string, requestBy string) error {
    c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
    if err != nil {
        return err
    }
    defer c.Close()

    return c.SignalWorkflow(
        context.Background(),
        workflowID,
        "",
        signals.CancelOrderSignal,
        signals.CancelOrderRequest{
            Reason:    reason,
            RequestBy: requestBy,
            Timestamp: time.Now().Unix(),
        },
    )
}

// Usage
err := cancelOrder("order-123", "Customer changed mind", "customer-456")
```

### Example: Send via CLI
```bash
temporal workflow signal \
    --workflow-id order-123 \
    --name cancel-order \
    --input '{"Reason":"Customer cancelled","RequestBy":"customer-456","Timestamp":1773392604}'
```

### Response
Workflow completes with:
```json
{
  "OrderID": "order-123",
  "Status": "CANCELLED",
  "Message": "Order cancelled: Customer changed mind (by customer-456)"
}
```

---

## Signal: update_shipping_address

### Purpose
Updates shipping address in workflow state without restarting the workflow.

### When to Use
- Customer updates delivery address
- Address validation correction
- Shipping preference change
- Before shipment is created

### Payload
```json
{
  "Name": "Jane Doe",
  "Street": "789 New Address Blvd",
  "City": "Los Angeles",
  "State": "CA",
  "PostalCode": "90001",
  "Country": "USA",
  "Phone": "555-9999",
  "UpdatedBy": "customer-456",
  "Timestamp": 1773392604
}
```

### Behavior
- Updates workflow state immediately
- Address used when creating shipment
- Multiple updates supported (last wins)
- No workflow restart required
- State persisted in history

### Example: Send via Go SDK
```go
func updateShippingAddress(workflowID string, address signals.UpdateShippingAddressRequest) error {
    c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
    if err != nil {
        return err
    }
    defer c.Close()

    return c.SignalWorkflow(
        context.Background(),
        workflowID,
        "",
        signals.UpdateShippingAddressSignal,
        address,
    )
}

// Usage
err := updateShippingAddress("order-123", signals.UpdateShippingAddressRequest{
    Name:       "Jane Doe",
    Street:     "789 New St",
    City:       "Los Angeles",
    State:      "CA",
    PostalCode: "90001",
    Country:    "USA",
    Phone:      "555-9999",
    UpdatedBy:  "customer-456",
    Timestamp:  time.Now().Unix(),
})
```

### Example: Send via CLI
```bash
temporal workflow signal \
    --workflow-id order-123 \
    --name update-shipping-address \
    --input '{"Name":"Jane Doe","Street":"789 New St","City":"Los Angeles","State":"CA","PostalCode":"90001","Country":"USA","Phone":"555-9999","UpdatedBy":"customer-456","Timestamp":1773392604}'
```

### Response
Workflow continues normally, using updated address for shipment creation.

---

## Common Scenarios

### Scenario 1: Cancel Order Early
```go
// Customer cancels immediately after placing order
err := c.SignalWorkflow(ctx, "order-123", "", signals.CancelOrderSignal,
    signals.CancelOrderRequest{
        Reason:    "Customer changed mind",
        RequestBy: "customer-456",
        Timestamp: time.Now().Unix(),
    })

// Result: Order cancelled, no compensation needed
```

### Scenario 2: Update Address Before Shipment
```go
// Customer updates address after payment but before shipment
err := c.SignalWorkflow(ctx, "order-123", "", signals.UpdateShippingAddressSignal,
    signals.UpdateShippingAddressRequest{
        Name:       "Jane Doe",
        Street:     "456 New Address",
        City:       "Seattle",
        State:      "WA",
        PostalCode: "98101",
        Country:    "USA",
        Phone:      "555-1234",
        UpdatedBy:  "customer-456",
        Timestamp:  time.Now().Unix(),
    })

// Result: Shipment created with new address
```

### Scenario 3: Multiple Address Updates
```go
// Customer updates address multiple times
// First update
c.SignalWorkflow(ctx, "order-123", "", signals.UpdateShippingAddressSignal, address1)

// Second update (overrides first)
c.SignalWorkflow(ctx, "order-123", "", signals.UpdateShippingAddressSignal, address2)

// Result: Last address (address2) is used
```

### Scenario 4: Cancel After Payment
```go
// Fraud system detects suspicious activity after payment
err := c.SignalWorkflow(ctx, "order-123", "", signals.CancelOrderSignal,
    signals.CancelOrderRequest{
        Reason:    "Fraudulent transaction detected",
        RequestBy: "fraud-detection-system",
        Timestamp: time.Now().Unix(),
    })

// Result: Payment refunded, inventory released
```

---

## Best Practices

### ✅ DO

1. **Always include RequestBy** - For audit trail
2. **Use current timestamp** - For tracking
3. **Provide clear reasons** - For cancellations
4. **Validate addresses** - Before sending update
5. **Handle errors** - Signal sending can fail
6. **Check workflow status** - Before sending signals
7. **Log all signals** - For monitoring

### ❌ DON'T

1. **Don't send signals to completed workflows** - They'll be ignored
2. **Don't assume immediate processing** - Signals are queued
3. **Don't send duplicate signals** - Can cause confusion
4. **Don't update address after shipment** - Too late
5. **Don't cancel without reason** - Breaks audit trail
6. **Don't ignore errors** - Signal delivery can fail
7. **Don't send signals too frequently** - Rate limit

---

## Error Handling

### Signal Delivery Errors
```go
err := c.SignalWorkflow(ctx, workflowID, "", signalName, payload)
if err != nil {
    switch {
    case strings.Contains(err.Error(), "workflow not found"):
        // Workflow doesn't exist or already completed
        log.Error("Workflow not found", "workflowID", workflowID)
    case strings.Contains(err.Error(), "timeout"):
        // Temporal server timeout
        log.Error("Signal timeout", "workflowID", workflowID)
    default:
        // Other errors
        log.Error("Signal failed", "error", err)
    }
    return err
}
```

### Checking Workflow Status Before Signaling
```go
// Get workflow description
desc, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
if err != nil {
    return err
}

// Check if workflow is still running
if desc.WorkflowExecutionInfo.Status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
    return fmt.Errorf("workflow not running: %v", desc.WorkflowExecutionInfo.Status)
}

// Safe to send signal
err = c.SignalWorkflow(ctx, workflowID, "", signalName, payload)
```

---

## Monitoring

### Metrics to Track

1. **Signal Delivery Rate**
   - Signals sent per minute
   - Success vs failure rate
   - Latency

2. **Cancellation Metrics**
   - Cancellations by stage
   - Cancellation reasons
   - Compensation success rate

3. **Address Update Metrics**
   - Updates per order
   - Update timing (before/after payment)
   - Update frequency

### Example Prometheus Metrics
```go
var (
    signalsSent = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "temporal_signals_sent_total",
            Help: "Total number of signals sent",
        },
        []string{"signal_name", "status"},
    )
    
    cancellationsByStage = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "order_cancellations_by_stage_total",
            Help: "Order cancellations by workflow stage",
        },
        []string{"stage", "reason"},
    )
)
```

---

## Troubleshooting

### Signal Not Processed

**Problem:** Signal sent but workflow doesn't respond

**Solutions:**
1. Check workflow is still running
2. Verify signal name matches exactly
3. Check payload structure
4. Review workflow logs
5. Check Temporal UI for signal history

### Cancellation Not Working

**Problem:** Cancel signal sent but order continues

**Solutions:**
1. Check if workflow already completed
2. Verify compensation activities are registered
3. Review workflow logs for errors
4. Check if signal arrived too late

### Address Update Not Applied

**Problem:** Address updated but old address used

**Solutions:**
1. Check timing - update before shipment?
2. Verify signal payload structure
3. Check workflow logs for signal receipt
4. Ensure multiple updates (last should win)

---

## Testing Signals

### Unit Test Example
```go
func TestCancelSignal(t *testing.T) {
    testSuite := &testsuite.WorkflowTestSuite{}
    env := testSuite.NewTestWorkflowEnvironment()
    
    // Register workflows and activities
    env.RegisterWorkflow(OrderWorkflow)
    env.RegisterActivity(inventoryActivity.ReserveInventory)
    
    // Send signal during test
    env.RegisterDelayedCallback(func() {
        env.SignalWorkflow(signals.CancelOrderSignal, 
            signals.CancelOrderRequest{
                Reason:    "Test cancellation",
                RequestBy: "test",
                Timestamp: time.Now().Unix(),
            })
    }, time.Millisecond*50)
    
    // Execute workflow
    env.ExecuteWorkflow(OrderWorkflow, input)
    
    // Verify result
    var result OrderWorkflowResult
    env.GetWorkflowResult(&result)
    assert.Equal(t, "CANCELLED", result.Status)
}
```

---

## Summary

### Signals Available
- ✅ **cancel_order** - Cancel with compensation
- ✅ **update_shipping_address** - Update address

### Key Points
- Signals are asynchronous and durable
- Signals are queued if workflow is busy
- Signals don't restart workflows
- State updates are persisted
- Deterministic and replay-safe

### When to Use
- **cancel_order**: Customer cancellation, fraud detection, admin intervention
- **update_shipping_address**: Address correction, customer update, before shipment

### Production Ready
- ✅ Error handling
- ✅ Compensation logic
- ✅ Audit logging
- ✅ Monitoring support
- ✅ Comprehensive tests

**For detailed implementation, see SIGNALS_COMPLETE.md**
