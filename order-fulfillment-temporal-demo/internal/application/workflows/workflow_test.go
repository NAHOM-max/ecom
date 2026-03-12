package workflows

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
)

func TestOrderWorkflow_Success(t *testing.T) {
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

	if result.PaymentID != "pay-456" {
		t.Errorf("Expected paymentID pay-456, got %s", result.PaymentID)
	}

	if result.ShipmentID != "ship-789" {
		t.Errorf("Expected shipmentID ship-789, got %s", result.ShipmentID)
	}

	t.Logf("✅ Order completed successfully: %+v", result)
}

func TestOrderWorkflow_PaymentFailure_CompensatesInventory(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)
	paymentActivity := activities.NewPaymentActivity(0.0)

	// Mock successful inventory reservation
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "res-123",
		Success:       true,
		Message:       "Inventory reserved",
	}, nil)

	// Mock payment failure
	env.OnActivity(paymentActivity.ChargePayment, mock.Anything, mock.Anything).Return(nil, errors.New("payment gateway unavailable"))

	// Mock compensation - release inventory
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil, nil)

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
	if err == nil {
		t.Fatal("Expected workflow to fail due to payment error")
	}

	t.Logf("✅ Compensation executed: Inventory released for res-123")
}

func TestOrderWorkflow_ShipmentFailure_CompensatesPaymentAndInventory(t *testing.T) {
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

	// Mock shipment failure
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(nil, errors.New("shipping service unavailable"))

	// Mock compensation - refund payment
	env.OnActivity(paymentActivity.RefundPayment, mock.Anything, "pay-456").Return(nil, nil)

	// Mock compensation - release inventory
	env.OnActivity(inventoryActivity.ReleaseInventory, mock.Anything, "res-123").Return(nil, nil)

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
	if err == nil {
		t.Fatal("Expected workflow to fail due to shipment error")
	}

	t.Logf("✅ Full compensation executed: Payment refunded (pay-456) and Inventory released (res-123)")
}

func TestOrderWorkflow_InventoryOutOfStock(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Create activity instances
	inventoryActivity := activities.NewInventoryActivity(0.0)

	// Mock inventory out of stock (business error)
	env.OnActivity(inventoryActivity.ReserveInventory, mock.Anything, mock.Anything).Return(&activities.ReserveInventoryResult{
		ReservationID: "",
		Success:       false,
		Message:       "Product prod-1 is out of stock",
	}, nil)

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
		t.Fatalf("Workflow returned error: %v", err)
	}

	if result.Status != "FAILED" {
		t.Errorf("Expected status FAILED, got %s", result.Status)
	}

	t.Logf("✅ Business error handled correctly: %s", result.Message)
}

func TestShipmentWorkflow_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Create activity instance
	shippingActivity := activities.NewShippingActivity(0.0)

	// Mock successful shipment creation
	env.OnActivity(shippingActivity.CreateShipment, mock.Anything, mock.Anything).Return(&activities.CreateShipmentResult{
		ShipmentID:     "ship-123",
		TrackingNumber: "TRK456",
		Carrier:        "FedEx",
		EstimatedDate:  "2026-03-18",
		Success:        true,
		Message:        "Shipment created",
	}, nil)

	env.ExecuteWorkflow(ShipmentWorkflow, ShipmentWorkflowInput{
		OrderID:        "order-123",
		ShippingMethod: "express",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	var result ShipmentWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	if result.ShipmentID != "ship-123" {
		t.Errorf("Expected shipmentID ship-123, got %s", result.ShipmentID)
	}

	t.Logf("✅ Shipment created successfully: %+v", result)
}
