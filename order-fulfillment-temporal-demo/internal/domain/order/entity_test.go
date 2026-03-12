package order

import (
	"testing"
	"time"
)

func TestNewOrder(t *testing.T) {
	tests := []struct {
		name       string
		customerID string
		items      []OrderItem
		wantErr    bool
	}{
		{
			name:       "valid order",
			customerID: "cust-123",
			items: []OrderItem{
				{ProductID: "prod-1", Quantity: 2, Price: 10.0},
			},
			wantErr: false,
		},
		{
			name:       "empty customer ID",
			customerID: "",
			items: []OrderItem{
				{ProductID: "prod-1", Quantity: 1, Price: 10.0},
			},
			wantErr: true,
		},
		{
			name:       "no items",
			customerID: "cust-123",
			items:      []OrderItem{},
			wantErr:    true,
		},
		{
			name:       "negative quantity",
			customerID: "cust-123",
			items: []OrderItem{
				{ProductID: "prod-1", Quantity: -1, Price: 10.0},
			},
			wantErr: true,
		},
		{
			name:       "negative price",
			customerID: "cust-123",
			items: []OrderItem{
				{ProductID: "prod-1", Quantity: 1, Price: -10.0},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, err := NewOrder(tt.customerID, tt.items)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOrder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && order.Status != OrderStatusCreated {
				t.Errorf("NewOrder() status = %v, want %v", order.Status, OrderStatusCreated)
			}
		})
	}
}

func TestOrder_CalculateTotal(t *testing.T) {
	order := &Order{
		Items: []OrderItem{
			{ProductID: "prod-1", Quantity: 2, Price: 10.0},
			{ProductID: "prod-2", Quantity: 3, Price: 5.0},
		},
	}

	total := order.CalculateTotal()
	expected := 35.0

	if total != expected {
		t.Errorf("CalculateTotal() = %v, want %v", total, expected)
	}
}

func TestOrder_ReserveInventory(t *testing.T) {
	order := &Order{
		Status:        OrderStatusCreated,
		InventoryHeld: false,
		UpdatedAt:     time.Now(),
	}

	err := order.ReserveInventory()
	if err != nil {
		t.Errorf("ReserveInventory() error = %v", err)
	}

	if order.Status != OrderStatusInventoryReserved {
		t.Errorf("Status = %v, want %v", order.Status, OrderStatusInventoryReserved)
	}

	if !order.InventoryHeld {
		t.Error("InventoryHeld should be true")
	}

	// Test idempotency guard
	err = order.ReserveInventory()
	if err == nil {
		t.Error("Expected error when reserving inventory twice")
	}
}

func TestOrder_MarkPaymentCharged(t *testing.T) {
	order := &Order{
		Status:    OrderStatusInventoryReserved,
		UpdatedAt: time.Now(),
	}

	err := order.MarkPaymentCharged("pay-123")
	if err != nil {
		t.Errorf("MarkPaymentCharged() error = %v", err)
	}

	if order.Status != OrderStatusPaymentCharged {
		t.Errorf("Status = %v, want %v", order.Status, OrderStatusPaymentCharged)
	}

	if order.PaymentID != "pay-123" {
		t.Errorf("PaymentID = %v, want pay-123", order.PaymentID)
	}

	if order.PaymentStatus != PaymentStatusCharged {
		t.Errorf("PaymentStatus = %v, want %v", order.PaymentStatus, PaymentStatusCharged)
	}
}

func TestOrder_StartShipment(t *testing.T) {
	order := &Order{
		Status:    OrderStatusPaymentCharged,
		UpdatedAt: time.Now(),
	}

	err := order.StartShipment("ship-456")
	if err != nil {
		t.Errorf("StartShipment() error = %v", err)
	}

	if order.Status != OrderStatusShipping {
		t.Errorf("Status = %v, want %v", order.Status, OrderStatusShipping)
	}

	if order.ShipmentID != "ship-456" {
		t.Errorf("ShipmentID = %v, want ship-456", order.ShipmentID)
	}
}

func TestOrder_CompleteOrder(t *testing.T) {
	order := &Order{
		Status:    OrderStatusShipping,
		UpdatedAt: time.Now(),
	}

	err := order.CompleteOrder()
	if err != nil {
		t.Errorf("CompleteOrder() error = %v", err)
	}

	if order.Status != OrderStatusCompleted {
		t.Errorf("Status = %v, want %v", order.Status, OrderStatusCompleted)
	}

	if order.ShipmentStatus != ShipmentStatusDelivered {
		t.Errorf("ShipmentStatus = %v, want %v", order.ShipmentStatus, ShipmentStatusDelivered)
	}
}

func TestOrder_CancelOrder(t *testing.T) {
	tests := []struct {
		name    string
		status  OrderStatus
		wantErr bool
	}{
		{"cancel created order", OrderStatusCreated, false},
		{"cancel inventory reserved", OrderStatusInventoryReserved, false},
		{"cannot cancel completed", OrderStatusCompleted, true},
		{"cannot cancel already cancelled", OrderStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{
				Status:    tt.status,
				UpdatedAt: time.Now(),
			}

			err := order.CancelOrder()
			if (err != nil) != tt.wantErr {
				t.Errorf("CancelOrder() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && order.Status != OrderStatusCancelled {
				t.Errorf("Status = %v, want %v", order.Status, OrderStatusCancelled)
			}
		})
	}
}

func TestOrder_CanBeCancelled(t *testing.T) {
	tests := []struct {
		name   string
		status OrderStatus
		want   bool
	}{
		{"created can be cancelled", OrderStatusCreated, true},
		{"inventory reserved can be cancelled", OrderStatusInventoryReserved, true},
		{"payment charged can be cancelled", OrderStatusPaymentCharged, true},
		{"shipping can be cancelled", OrderStatusShipping, true},
		{"completed cannot be cancelled", OrderStatusCompleted, false},
		{"cancelled cannot be cancelled", OrderStatusCancelled, false},
		{"failed cannot be cancelled", OrderStatusFailed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{Status: tt.status}
			if got := order.CanBeCancelled(); got != tt.want {
				t.Errorf("CanBeCancelled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrder_StateTransitions(t *testing.T) {
	// Test complete happy path
	order := &Order{
		CustomerID: "cust-123",
		Items: []OrderItem{
			{ProductID: "prod-1", Quantity: 1, Price: 10.0},
		},
		Status:    OrderStatusCreated,
		UpdatedAt: time.Now(),
	}

	// Reserve inventory
	if err := order.ReserveInventory(); err != nil {
		t.Fatalf("ReserveInventory() failed: %v", err)
	}

	// Charge payment
	if err := order.MarkPaymentCharged("pay-123"); err != nil {
		t.Fatalf("MarkPaymentCharged() failed: %v", err)
	}

	// Start shipment
	if err := order.StartShipment("ship-456"); err != nil {
		t.Fatalf("StartShipment() failed: %v", err)
	}

	// Complete order
	if err := order.CompleteOrder(); err != nil {
		t.Fatalf("CompleteOrder() failed: %v", err)
	}

	if order.Status != OrderStatusCompleted {
		t.Errorf("Final status = %v, want %v", order.Status, OrderStatusCompleted)
	}
}
