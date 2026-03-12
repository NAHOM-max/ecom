package order

import (
	"errors"
	"time"
)

// OrderStatus represents the current state of an order
type OrderStatus string

const (
	OrderStatusCreated           OrderStatus = "CREATED"
	OrderStatusInventoryReserved OrderStatus = "INVENTORY_RESERVED"
	OrderStatusPaymentCharged    OrderStatus = "PAYMENT_CHARGED"
	OrderStatusShipping          OrderStatus = "SHIPPING"
	OrderStatusCompleted         OrderStatus = "COMPLETED"
	OrderStatusCancelled         OrderStatus = "CANCELLED"
	OrderStatusFailed            OrderStatus = "FAILED"
)

// PaymentStatus represents payment state
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "PENDING"
	PaymentStatusCharged   PaymentStatus = "CHARGED"
	PaymentStatusRefunded  PaymentStatus = "REFUNDED"
	PaymentStatusFailed    PaymentStatus = "FAILED"
)

// ShipmentStatus represents shipment state
type ShipmentStatus string

const (
	ShipmentStatusPending   ShipmentStatus = "PENDING"
	ShipmentStatusCreated   ShipmentStatus = "CREATED"
	ShipmentStatusInTransit ShipmentStatus = "IN_TRANSIT"
	ShipmentStatusDelivered ShipmentStatus = "DELIVERED"
	ShipmentStatusCancelled ShipmentStatus = "CANCELLED"
)

// Order domain entity
type Order struct {
	ID             string
	CustomerID     string
	Items          []OrderItem
	TotalAmount    float64
	Status         OrderStatus
	PaymentStatus  PaymentStatus
	PaymentID      string
	ShipmentStatus ShipmentStatus
	ShipmentID     string
	InventoryHeld  bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// OrderItem represents a line item in an order
type OrderItem struct {
	ProductID string
	Quantity  int
	Price     float64
}

// NewOrder creates a new order with validation
func NewOrder(customerID string, items []OrderItem) (*Order, error) {
	if customerID == "" {
		return nil, errors.New("customer ID is required")
	}
	if len(items) == 0 {
		return nil, errors.New("order must have at least one item")
	}

	order := &Order{
		CustomerID:     customerID,
		Items:          items,
		Status:         OrderStatusCreated,
		PaymentStatus:  PaymentStatusPending,
		ShipmentStatus: ShipmentStatusPending,
		InventoryHeld:  false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := order.Validate(); err != nil {
		return nil, err
	}

	order.TotalAmount = order.CalculateTotal()
	return order, nil
}

// Validate performs business validation
func (o *Order) Validate() error {
	if o.CustomerID == "" {
		return errors.New("customer ID is required")
	}
	if len(o.Items) == 0 {
		return errors.New("order must have at least one item")
	}
	for i, item := range o.Items {
		if item.ProductID == "" {
			return errors.New("product ID is required for all items")
		}
		if item.Quantity <= 0 {
			return errors.New("item quantity must be positive")
		}
		if item.Price < 0 {
			return errors.New("item price cannot be negative")
		}
		if i != 0 && item.ProductID == o.Items[i-1].ProductID {
			return errors.New("duplicate product IDs not allowed")
		}
	}
	return nil
}

// CalculateTotal computes the total amount
func (o *Order) CalculateTotal() float64 {
	total := 0.0
	for _, item := range o.Items {
		total += float64(item.Quantity) * item.Price
	}
	return total
}

// ReserveInventory marks inventory as reserved
func (o *Order) ReserveInventory() error {
	if o.Status != OrderStatusCreated {
		return errors.New("can only reserve inventory for created orders")
	}
	if o.InventoryHeld {
		return errors.New("inventory already reserved")
	}
	o.InventoryHeld = true
	o.Status = OrderStatusInventoryReserved
	o.UpdatedAt = time.Now()
	return nil
}

// MarkPaymentCharged marks payment as charged
func (o *Order) MarkPaymentCharged(paymentID string) error {
	if o.Status != OrderStatusInventoryReserved {
		return errors.New("can only charge payment after inventory is reserved")
	}
	if paymentID == "" {
		return errors.New("payment ID is required")
	}
	o.PaymentID = paymentID
	o.PaymentStatus = PaymentStatusCharged
	o.Status = OrderStatusPaymentCharged
	o.UpdatedAt = time.Now()
	return nil
}

// StartShipment marks shipment as started
func (o *Order) StartShipment(shipmentID string) error {
	if o.Status != OrderStatusPaymentCharged {
		return errors.New("can only start shipment after payment is charged")
	}
	if shipmentID == "" {
		return errors.New("shipment ID is required")
	}
	o.ShipmentID = shipmentID
	o.ShipmentStatus = ShipmentStatusCreated
	o.Status = OrderStatusShipping
	o.UpdatedAt = time.Now()
	return nil
}

// CompleteOrder marks order as completed
func (o *Order) CompleteOrder() error {
	if o.Status != OrderStatusShipping {
		return errors.New("can only complete order after shipment started")
	}
	o.Status = OrderStatusCompleted
	o.ShipmentStatus = ShipmentStatusDelivered
	o.UpdatedAt = time.Now()
	return nil
}

// CancelOrder cancels the order
func (o *Order) CancelOrder() error {
	if !o.CanBeCancelled() {
		return errors.New("order cannot be cancelled in current state")
	}
	o.Status = OrderStatusCancelled
	o.UpdatedAt = time.Now()
	return nil
}

// MarkFailed marks order as failed
func (o *Order) MarkFailed() error {
	if o.Status == OrderStatusCompleted || o.Status == OrderStatusCancelled {
		return errors.New("cannot mark completed or cancelled order as failed")
	}
	o.Status = OrderStatusFailed
	o.UpdatedAt = time.Now()
	return nil
}

// CanBeCancelled checks if order can be cancelled
func (o *Order) CanBeCancelled() bool {
	return o.Status != OrderStatusCompleted &&
		o.Status != OrderStatusCancelled &&
		o.Status != OrderStatusFailed
}

// IsTerminalState checks if order is in a terminal state
func (o *Order) IsTerminalState() bool {
	return o.Status == OrderStatusCompleted ||
		o.Status == OrderStatusCancelled ||
		o.Status == OrderStatusFailed
}
