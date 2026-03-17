package http

import (
	"net/http"
)

// NewRouter builds and returns an http.Handler with all order routes registered.
//
// Routes:
//
//	POST   /orders                      → CreateOrder
//	GET    /orders/{id}/status          → GetOrderStatus
//	POST   /orders/{id}/cancel          → CancelOrder
//	POST   /orders/{id}/priority        → SetOrderPriority
//	POST   /orders/{id}/change_address  → ChangeAddress
func NewRouter(h *OrderHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /orders", h.CreateOrder)
	mux.HandleFunc("GET /orders/{id}/status", h.GetOrderStatus)
	mux.HandleFunc("POST /orders/{id}/cancel", h.CancelOrder)
	mux.HandleFunc("POST /orders/{id}/priority", h.SetOrderPriority)
	mux.HandleFunc("POST /orders/{id}/change_address", h.ChangeAddress)

	return mux
}
