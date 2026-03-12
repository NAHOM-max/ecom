package activities

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestInventoryActivity_ReserveInventory(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	// Create activity with 0% failure rate for testing
	act := NewInventoryActivity(0.0)
	env.RegisterActivity(act.ReserveInventory)

	input := ReserveInventoryInput{
		OrderID: "order-123",
		Items: []InventoryItem{
			{ProductID: "prod-1", Quantity: 2},
			{ProductID: "prod-2", Quantity: 1},
		},
	}

	val, err := env.ExecuteActivity(act.ReserveInventory, input)
	if err != nil {
		t.Fatalf("ReserveInventory failed: %v", err)
	}

	var result ReserveInventoryResult
	if err := val.Get(&result); err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	if result.ReservationID == "" {
		t.Error("Expected non-empty reservation ID")
	}
}

func TestPaymentActivity_ChargePayment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	act := NewPaymentActivity(0.0)
	env.RegisterActivity(act.ChargePayment)

	input := ChargePaymentInput{
		OrderID:    "order-123",
		CustomerID: "cust-456",
		Amount:     99.99,
		Currency:   "USD",
	}

	val, err := env.ExecuteActivity(act.ChargePayment, input)
	if err != nil {
		t.Fatalf("ChargePayment failed: %v", err)
	}

	var result ChargePaymentResult
	if err := val.Get(&result); err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	if result.Status != "charged" {
		t.Errorf("Expected status=charged, got %v", result.Status)
	}

	if result.PaymentID == "" {
		t.Error("Expected non-empty payment ID")
	}
}

func TestShippingActivity_CreateShipment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	act := NewShippingActivity(0.0)
	env.RegisterActivity(act.CreateShipment)

	input := CreateShipmentInput{
		OrderID: "order-123",
		CustomerAddress: ShippingAddress{
			Name:       "John Doe",
			Street:     "123 Main St",
			City:       "New York",
			State:      "NY",
			PostalCode: "10001",
			Country:    "USA",
		},
		Items: []ShippingItem{
			{ProductID: "prod-1", Quantity: 2, Weight: 1.5},
		},
		ShippingMethod: "standard",
	}

	val, err := env.ExecuteActivity(act.CreateShipment, input)
	if err != nil {
		t.Fatalf("CreateShipment failed: %v", err)
	}

	var result CreateShipmentResult
	if err := val.Get(&result); err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	if result.ShipmentID == "" {
		t.Error("Expected non-empty shipment ID")
	}
}

func TestInventoryActivity_WithFailures(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	act := NewInventoryActivity(1.0)
	env.RegisterActivity(act.ReserveInventory)

	input := ReserveInventoryInput{
		OrderID: "order-123",
		Items: []InventoryItem{
			{ProductID: "prod-1", Quantity: 1},
		},
	}

	_, err := env.ExecuteActivity(act.ReserveInventory, input)
	if err == nil {
		t.Error("Expected error with 100% failure rate, got nil")
	}
}
