package workflows

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/queries"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
)

func queryOrderStatus(t *testing.T, env *testsuite.TestWorkflowEnvironment) queries.OrderStatusResult {
	t.Helper()
	val, err := env.QueryWorkflow(queries.OrderStatusQuery)
	if err != nil {
		t.Fatalf("order_status query failed: %v", err)
	}
	var result queries.OrderStatusResult
	if err := val.Get(&result); err != nil {
		t.Fatalf("failed to decode order_status result: %v", err)
	}
	return result
}

func TestOrderWorkflow_Query_CompletedOrder(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ShipmentWorkflow)

	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123", Success: true,
	}, nil)
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID: "pay-456", Status: "charged",
	}, nil)
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(&activities.CreateShipmentResult{
		ShipmentID: "ship-789", TrackingNumber: "TRK123", Carrier: "UPS", Success: true,
	}, nil)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items:      []OrderItemInput{{ProductID: "prod-1", Quantity: 1, Price: 29.99}},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	result := queryOrderStatus(t, env)

	if result.OrderID != "order-123" {
		t.Errorf("expected order_id order-123, got %s", result.OrderID)
	}
	if result.CurrentStatus != "COMPLETED" {
		t.Errorf("expected current_status COMPLETED, got %s", result.CurrentStatus)
	}
	if result.PaymentStatus != "charged" {
		t.Errorf("expected payment_status charged, got %s", result.PaymentStatus)
	}
	if result.ShipmentStatus != "created" {
		t.Errorf("expected shipment_status created, got %s", result.ShipmentStatus)
	}

	t.Logf("✅ order_status query on completed order: %+v", result)
}

func TestOrderWorkflow_Query_CancelledOrder(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ShipmentWorkflow)

	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123", Success: true,
	}, nil)
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID: "pay-456", Status: "charged",
	}, nil)
	// CreateShipment succeeds so the child workflow blocks on its 3s confirmation timer,
	// keeping the selector alive long enough for the cancel signal to arrive.
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(&activities.CreateShipmentResult{
		ShipmentID: "ship-789", TrackingNumber: "TRK123", Carrier: "UPS", Success: true,
	}, nil)
	env.OnActivity(paymentActivity.RefundPayment, mock.Anything, "pay-456").Return(nil)
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil)

	// Cancel arrives while the child workflow is waiting on its 3s confirmation timer.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "customer request",
			RequestBy: "customer-123",
			Timestamp: time.Now().Unix(),
		})
	}, time.Millisecond*500)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items:      []OrderItemInput{{ProductID: "prod-1", Quantity: 1, Price: 29.99}},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}

	result := queryOrderStatus(t, env)

	if result.CurrentStatus != "CANCELLED" {
		t.Errorf("expected current_status CANCELLED, got %s", result.CurrentStatus)
	}
	if result.PaymentStatus != "refunded" {
		t.Errorf("expected payment_status refunded, got %s", result.PaymentStatus)
	}

	t.Logf("✅ order_status query on cancelled order: %+v", result)
}
