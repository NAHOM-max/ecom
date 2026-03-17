package http

import (
	"encoding/json"
	"net/http"
	"time"

	temporal "github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/updates"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
)

type OrderHandler struct {
	temporalClient *temporal.Client
}

func NewOrderHandler(temporalClient *temporal.Client) *OrderHandler {
	return &OrderHandler{temporalClient: temporalClient}
}

// POST /orders
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var input workflows.OrderWorkflowInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.OrderID == "" || input.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "order_id and customer_id are required")
		return
	}

	run, err := h.temporalClient.StartOrderWorkflow(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"workflow_id": run.GetID(),
		"run_id":      run.GetRunID(),
	})
}

// GET /orders/{id}/status
func (h *OrderHandler) GetOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderID := orderIDFromPath(r)
	if orderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required")
		return
	}

	result, err := h.temporalClient.QueryOrderStatus(r.Context(), "order-"+orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// POST /orders/{id}/cancel
func (h *OrderHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID := orderIDFromPath(r)
	if orderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required")
		return
	}

	var req signals.CancelOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Timestamp = time.Now().Unix()

	if err := h.temporalClient.CancelOrder(r.Context(), "order-"+orderID, req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancel signal sent"})
}

// POST /orders/{id}/priority
func (h *OrderHandler) SetOrderPriority(w http.ResponseWriter, r *http.Request) {
	orderID := orderIDFromPath(r)
	if orderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required")
		return
	}

	var input updates.SetPriorityInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := input.Priority.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.temporalClient.UpdateOrderPriority(r.Context(), "order-"+orderID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// POST /orders/{id}/change_address
func (h *OrderHandler) ChangeAddress(w http.ResponseWriter, r *http.Request) {
	orderID := orderIDFromPath(r)
	if orderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required")
		return
	}

	var req signals.UpdateShippingAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Timestamp = time.Now().Unix()

	if err := h.temporalClient.ChangeShippingAddress(r.Context(), "order-"+orderID, req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "address update signal sent"})
}

// orderIDFromPath extracts the {id} segment from /orders/{id}/...
func orderIDFromPath(r *http.Request) string {
	// PathValue is available in Go 1.22+; fall back to manual parse for 1.21.
	if id := r.PathValue("id"); id != "" {
		return id
	}
	// manual fallback: /orders/{id}/...
	parts := splitPath(r.URL.Path)
	for i, p := range parts {
		if p == "orders" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if seg := path[start:i]; seg != "" {
				parts = append(parts, seg)
			}
			start = i + 1
		}
	}
	return parts
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
