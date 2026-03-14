package queries

// Queries allow external systems to read workflow state without modifying it
// Used for:
// - Getting current order status
// - Retrieving workflow progress
// - Debugging and monitoring
// - Customer status checks
//
// Queries are synchronous and read-only - they don't affect workflow execution

// Query names as constants for type safety
const (
	// OrderStatusQuery is the query name exposed to the API layer
	OrderStatusQuery = "order_status"

	// GetOrderStatusQuery retrieves current order status
	GetOrderStatusQuery = "get-order-status"

	// GetOrderStateQuery retrieves complete order state
	GetOrderStateQuery = "get-order-state"

	// GetWorkflowProgressQuery retrieves workflow execution progress
	GetWorkflowProgressQuery = "get-workflow-progress"

	// GetCompletedStepsQuery retrieves list of completed steps
	GetCompletedStepsQuery = "get-completed-steps"
)

// OrderStatusResult is the response returned by the order_status query
type OrderStatusResult struct {
	OrderID        string `json:"order_id"`
	CurrentStatus  string `json:"current_status"`
	PaymentStatus  string `json:"payment_status"`
	ShipmentStatus string `json:"shipment_status"`
	Priority       string `json:"priority"`
}

// OrderStatusResponse contains order status information
type OrderStatusResponse struct {
	OrderID     string
	Status      string
	LastUpdated int64
}

// OrderStateResponse contains complete order state
type OrderStateResponse struct {
	OrderID        string
	Status         string
	InventoryHeld  bool
	PaymentID      string
	PaymentStatus  string
	ShipmentID     string
	ShipmentStatus string
	LastUpdated    int64
	CreatedAt      int64
}

// WorkflowProgressResponse contains workflow execution progress
type WorkflowProgressResponse struct {
	TotalSteps     int
	CompletedSteps int
	CurrentStep    string
	PercentDone    float64
	EstimatedTime  int64
}

// CompletedStepsResponse contains list of completed steps
type CompletedStepsResponse struct {
	Steps []StepInfo
}

type StepInfo struct {
	Name        string
	Status      string
	CompletedAt int64
	Duration    int64
}
