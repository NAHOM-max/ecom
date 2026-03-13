package signals

// Signals allow external systems to send messages to running workflows
// Used for:
// - Order cancellation requests
// - Order updates from external events
// - Customer-initiated changes
// - Admin interventions
//
// Signals are asynchronous and durable - they're queued if workflow is busy

// Signal names as constants for type safety
const (
	// CancelOrderSignal requests order cancellation
	CancelOrderSignal = "cancel-order"

	// UpdateOrderSignal sends order updates
	UpdateOrderSignal = "update-order"

	// UpdateShippingAddressSignal updates shipping address
	UpdateShippingAddressSignal = "update-shipping-address"

	// PaymentConfirmedSignal notifies payment confirmation from webhook
	PaymentConfirmedSignal = "payment-confirmed"

	// ShipmentStatusSignal updates shipment status from carrier
	ShipmentStatusSignal = "shipment-status"
)

// CancelOrderRequest contains cancellation request data
type CancelOrderRequest struct {
	Reason    string
	RequestBy string
	Timestamp int64
}

// UpdateShippingAddressRequest contains shipping address update data
type UpdateShippingAddressRequest struct {
	Name       string
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
	Phone      string
	UpdatedBy  string
	Timestamp  int64
}

// UpdateOrderRequest contains order update data
type UpdateOrderRequest struct {
	Field     string
	Value     interface{}
	UpdatedBy string
	Timestamp int64
}

// PaymentConfirmation contains payment webhook data
type PaymentConfirmation struct {
	PaymentID     string
	Status        string
	TransactionID string
	Timestamp     int64
}

// ShipmentStatusUpdate contains carrier status update
type ShipmentStatusUpdate struct {
	ShipmentID     string
	Status         string
	Location       string
	EstimatedDate  string
	Timestamp      int64
	TrackingEvents []TrackingEvent
}

type TrackingEvent struct {
	Status    string
	Location  string
	Timestamp int64
	Message   string
}
