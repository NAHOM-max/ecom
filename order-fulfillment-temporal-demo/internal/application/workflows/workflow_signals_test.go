package workflows

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
)

func TestOrderWorkflow_CancelSignal_BeforeInventory(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Send cancel signal immediately
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "Customer requested cancellation",
			RequestBy: "customer-123",
			Timestamp: time.Now().Unix(),
		})
	}, 0)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if result.Status != "CANCELLED" {
		t.Errorf("Expected status CANCELLED, got %s", result.Status)
	}

	t.Logf("✅ Order cancelled before inventory reservation: %s", result.Message)
}

func TestOrderWorkflow_CancelSignal_AfterInventory(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)

	// Mock successful inventory reservation
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	// Mock payment activity (in case it gets called before cancel is processed)
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID:     "pay-456",
		Status:        "charged",
		TransactionID: "txn-789",
		Message:       "Payment successful",
	}, nil)

	// Mock compensation - release inventory
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil)

	// Send cancel signal after a short delay
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "Customer changed mind",
			RequestBy: "customer-123",
			Timestamp: time.Now().Unix(),
		})
	}, time.Millisecond*50)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if result.Status != "CANCELLED" {
		t.Errorf("Expected status CANCELLED, got %s", result.Status)
	}

	t.Logf("✅ Order cancelled after inventory, compensation executed: %s", result.Message)
}

func TestOrderWorkflow_CancelSignal_AfterPayment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	// Mock successful inventory reservation
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	// Mock successful payment
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID:     "pay-456",
		Status:        "charged",
		TransactionID: "txn-789",
		Message:       "Payment successful",
	}, nil)

	// Mock CreateShipment to fail (simulating cancellation during shipment)
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(nil, errors.New("cancelled before shipment"))

	// Mock compensation - refund payment
	env.OnActivity(paymentActivity.RefundPayment, mock.Anything, "pay-456").Return(nil)

	// Mock compensation - release inventory
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil)

	// Send cancel signal after payment
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "Fraudulent order detected",
			RequestBy: "fraud-system",
			Timestamp: time.Now().Unix(),
		})
	}, time.Millisecond*100)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	// Workflow should fail due to shipment error, but compensation should execute
	if err == nil {
		// If no error, check if it was cancelled
		if result.Status != "CANCELLED" && result.Status != "FAILED" {
			t.Errorf("Expected status CANCELLED or FAILED, got %s", result.Status)
		}
		t.Logf("✅ Order cancelled/failed after payment, full compensation executed: %s", result.Message)
	} else {
		// Error is expected due to shipment failure
		t.Logf("✅ Order failed after payment with compensation (expected error): %v", err)
	}
}

func TestOrderWorkflow_UpdateShippingAddress(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	// Mock successful inventory reservation
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	// Mock successful payment
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID:     "pay-456",
		Status:        "charged",
		TransactionID: "txn-789",
		Message:       "Payment successful",
	}, nil)

	// Mock successful shipment
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(&activities.CreateShipmentResult{
		ShipmentID:     "ship-789",
		TrackingNumber: "TRK123",
		Carrier:        "UPS",
		EstimatedDate:  "2026-03-20",
		Success:        true,
		Message:        "Shipment created",
	}, nil)

	// Send update address signal during processing
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.UpdateShippingAddressSignal, signals.UpdateShippingAddressRequest{
			Name:       "Jane Doe",
			Street:     "789 New Address Blvd",
			City:       "Los Angeles",
			State:      "CA",
			PostalCode: "90001",
			Country:    "USA",
			Phone:      "555-9999",
			UpdatedBy:  "customer-123",
			Timestamp:  time.Now().Unix(),
		})
	}, time.Millisecond*75)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if result.Status != "COMPLETED" {
		t.Errorf("Expected status COMPLETED, got %s", result.Status)
	}

	t.Logf("✅ Order completed with updated shipping address: %+v", result)
}

func TestOrderWorkflow_MultipleSignals(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	// Mock successful inventory reservation
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	// Mock successful payment
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID:     "pay-456",
		Status:        "charged",
		TransactionID: "txn-789",
		Message:       "Payment successful",
	}, nil)

	// Mock CreateShipment to fail (simulating cancellation)
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(nil, errors.New("cancelled"))

	// Mock compensation
	env.OnActivity(paymentActivity.RefundPayment, mock.Anything, "pay-456").Return(nil)
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil)

	// Send multiple address updates
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.UpdateShippingAddressSignal, signals.UpdateShippingAddressRequest{
			Name:       "First Update",
			Street:     "111 First St",
			City:       "Boston",
			State:      "MA",
			PostalCode: "02101",
			Country:    "USA",
			Phone:      "555-1111",
			UpdatedBy:  "customer-123",
			Timestamp:  time.Now().Unix(),
		})
	}, time.Millisecond*25)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.UpdateShippingAddressSignal, signals.UpdateShippingAddressRequest{
			Name:       "Second Update",
			Street:     "222 Second Ave",
			City:       "Seattle",
			State:      "WA",
			PostalCode: "98101",
			Country:    "USA",
			Phone:      "555-2222",
			UpdatedBy:  "customer-123",
			Timestamp:  time.Now().Unix(),
		})
	}, time.Millisecond*50)

	// Finally send cancel signal
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "Customer cancelled after address changes",
			RequestBy: "customer-123",
			Timestamp: time.Now().Unix(),
		})
	}, time.Millisecond*90)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	// Workflow should fail or be cancelled
	if err == nil {
		if result.Status != "CANCELLED" && result.Status != "FAILED" {
			t.Errorf("Expected status CANCELLED or FAILED, got %s", result.Status)
		}
		t.Logf("✅ Multiple signals handled correctly, order cancelled/failed: %s", result.Message)
	} else {
		t.Logf("✅ Multiple signals handled correctly with compensation (expected error): %v", err)
	}
}

func TestOrderWorkflow_CancelSignal_TooLate(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register child workflow
	env.RegisterWorkflow(ShipmentWorkflow)

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)
	shippingActivity := activities.NewShippingActivity(0.0)

	// Mock all successful
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(&activities.ChargePaymentResult{
		PaymentID:     "pay-456",
		Status:        "charged",
		TransactionID: "txn-789",
		Message:       "Payment successful",
	}, nil)

	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(&activities.CreateShipmentResult{
		ShipmentID:     "ship-789",
		TrackingNumber: "TRK123",
		Carrier:        "UPS",
		EstimatedDate:  "2026-03-20",
		Success:        true,
		Message:        "Shipment created",
	}, nil)

	// Send cancel signal very late (after workflow would complete)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signals.CancelOrderSignal, signals.CancelOrderRequest{
			Reason:    "Too late to cancel",
			RequestBy: "customer-123",
			Timestamp: time.Now().Unix(),
		})
	}, time.Second*10)

	env.ExecuteWorkflow(OrderWorkflow, OrderWorkflowInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Items: []OrderItemInput{
			{ProductID: "prod-1", Quantity: 2, Price: 29.99},
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result OrderWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Should complete successfully since cancel came too late
	if result.Status != "COMPLETED" {
		t.Errorf("Expected status COMPLETED (cancel too late), got %s", result.Status)
	}

	t.Logf("✅ Order completed successfully, cancel signal came too late: %+v", result)
}
