package http

// Router configures HTTP routes and middleware
// Responsibilities:
// - Setup Gin router
// - Register routes
// - Configure middleware (logging, CORS, auth, etc.)
// - Health check endpoints

import (
	"github.com/gin-gonic/gin"
)

// Router wraps the Gin engine with application routes
type Router struct {
	engine       *gin.Engine
	orderHandler *OrderHandler
}

// NewRouter creates a new HTTP router
func NewRouter(orderHandler *OrderHandler) *Router {
	return &Router{
		engine:       gin.Default(),
		orderHandler: orderHandler,
	}
}

// Setup configures all routes and middleware
func (r *Router) Setup() {
	// TODO: Configure middleware
	// TODO: Setup CORS
	// TODO: Setup logging
	// TODO: Setup recovery

	// Health check
	r.engine.GET("/health", r.healthCheck)

	// API v1 routes
	v1 := r.engine.Group("/api/v1")
	{
		// Order routes
		orders := v1.Group("/orders")
		{
			orders.POST("", r.orderHandler.CreateOrder)
			orders.GET("", r.orderHandler.ListOrders)
			orders.GET("/:id", r.orderHandler.GetOrder)
			orders.PATCH("/:id", r.orderHandler.UpdateOrder)
			orders.POST("/:id/cancel", r.orderHandler.CancelOrder)
			orders.GET("/:id/status", r.orderHandler.GetOrderStatus)
			orders.GET("/:id/progress", r.orderHandler.GetOrderProgress)
		}
	}
}

// healthCheck handles health check requests
func (r *Router) healthCheck(c *gin.Context) {
	// TODO: Check dependencies health
	// TODO: Return health status
}

// Run starts the HTTP server
func (r *Router) Run(addr string) error {
	return r.engine.Run(addr)
}

// GetEngine returns the underlying Gin engine
func (r *Router) GetEngine() *gin.Engine {
	return r.engine
}
