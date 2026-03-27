package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/yourorg/order-fulfillment-temporal-demo/platform/observability"
)

// NewRouter builds and returns an http.Handler with all order routes registered.
//
// Routes:
//
//	GET    /health                      → health check
//	GET    /metrics                     → Prometheus metrics
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

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("POST /orders", h.CreateOrder)
	mux.HandleFunc("GET /orders/{id}/status", h.GetOrderStatus)
	mux.HandleFunc("POST /orders/{id}/cancel", h.CancelOrder)
	mux.HandleFunc("POST /orders/{id}/priority", h.SetOrderPriority)
	mux.HandleFunc("POST /orders/{id}/change_address", h.ChangeAddress)

	// Wrap the entire mux with:
	//   1. OTel HTTP tracing (adds a span per request)
	//   2. Prometheus HTTP metrics (records request count + duration)
	return observability.TraceHTTP(metricsMiddleware(mux))
}

// metricsMiddleware records http_requests_total and http_request_duration_seconds.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		observability.HTTPRequestsTotal.WithLabelValues(
			r.Method,
			r.URL.Path,
			strconv.Itoa(rw.status),
		).Inc()

		observability.HTTPDurationSeconds.WithLabelValues(
			r.Method,
			r.URL.Path,
		).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
