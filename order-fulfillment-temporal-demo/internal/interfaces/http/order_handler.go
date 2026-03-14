package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	temporal "github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
)

// OrderHandler handles HTTP requests for order operations
type OrderHandler struct {
	temporalClient *temporal.Client
}

// NewOrderHandler creates a new order handler
func NewOrderHandler(temporalClient *temporal.Client) *OrderHandler {
	return &OrderHandler{temporalClient: temporalClient}
}

// GetOrderStatus handles GET /orders/:id/status
// Queries the running workflow using the order_status query and returns the result.
func (h *OrderHandler) GetOrderStatus(c *gin.Context) {
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id is required"})
		return
	}

	result, err := h.temporalClient.QueryOrderStatus(c.Request.Context(), orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// CreateOrder handles POST /orders
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	// TODO: Parse request body
	// TODO: Validate input
	// TODO: Start order workflow
	// TODO: Return workflow ID and run ID
}

// GetOrder handles GET /orders/:id
func (h *OrderHandler) GetOrder(c *gin.Context) {
	// TODO: Get order ID from path
	// TODO: Query workflow state
	// TODO: Return order status
}

// CancelOrder handles POST /orders/:id/cancel
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	// TODO: Get order ID from path
	// TODO: Parse cancellation reason
	// TODO: Send cancel signal to workflow
	// TODO: Return success response
}

// UpdateOrder handles PATCH /orders/:id
func (h *OrderHandler) UpdateOrder(c *gin.Context) {
	// TODO: Get order ID from path
	// TODO: Parse update data
	// TODO: Send update signal to workflow
	// TODO: Return success response
}

// ListOrders handles GET /orders
func (h *OrderHandler) ListOrders(c *gin.Context) {
	// TODO: Parse query parameters
	// TODO: Query repository for orders
	// TODO: Return orders list
}

// GetOrderProgress handles GET /orders/:id/progress
func (h *OrderHandler) GetOrderProgress(c *gin.Context) {
	// TODO: Get order ID from path
	// TODO: Query workflow for progress
	// TODO: Return progress response
}
