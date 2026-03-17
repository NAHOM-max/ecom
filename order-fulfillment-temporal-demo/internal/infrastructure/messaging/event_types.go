package messaging

import "time"

// Event type constants
const (
	EventOrderCreated       = "OrderCreated"
	EventInventoryReserved  = "InventoryReserved"
	EventPaymentCharged     = "PaymentCharged"
	EventShipmentCreated    = "ShipmentCreated"
	EventOrderCompleted     = "OrderCompleted"
	EventOrderCancelled     = "OrderCancelled"
)

// Topic constants
const (
	TopicOrders    = "orders"
	TopicInventory = "inventory"
	TopicPayments  = "payments"
	TopicShipments = "shipments"
)

// Event is the envelope for all domain events published to Kafka.
type Event struct {
	EventID   string      `json:"event_id"`
	EventType string      `json:"event_type"`
	Timestamp time.Time   `json:"timestamp"`
	OrderID   string      `json:"order_id"`
	Payload   interface{} `json:"payload"`
}

// EventProducer is the abstraction activities depend on.
// Implementations can be Kafka, an in-memory stub, or a no-op.
type EventProducer interface {
	Publish(topic string, event Event) error
	Close() error
}

// --- Payload types ---

type OrderCreatedPayload struct {
	CustomerID string  `json:"customer_id"`
	TotalItems int     `json:"total_items"`
	TotalPrice float64 `json:"total_price"`
}

type InventoryReservedPayload struct {
	ReservationID string `json:"reservation_id"`
	ItemCount     int    `json:"item_count"`
}

type PaymentChargedPayload struct {
	PaymentID     string  `json:"payment_id"`
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
}

type ShipmentCreatedPayload struct {
	ShipmentID     string `json:"shipment_id"`
	TrackingNumber string `json:"tracking_number"`
	Carrier        string `json:"carrier"`
	EstimatedDate  string `json:"estimated_date"`
}

type OrderCompletedPayload struct {
	PaymentID  string `json:"payment_id"`
	ShipmentID string `json:"shipment_id"`
}

type OrderCancelledPayload struct {
	Reason    string `json:"reason"`
	RequestBy string `json:"request_by"`
}
