package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.temporal.io/sdk/interceptor"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
	infrahttp "github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/http"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/payment"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/shipment"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
	"github.com/yourorg/order-fulfillment-temporal-demo/platform/observability"
)

func main() {
	// --- Tracing ---
	otlpEndpoint := getEnv("OTLP_ENDPOINT", "localhost:4318")
	shutdownTracer, err := observability.InitTracer("order-fulfillment-worker", otlpEndpoint)
	if err != nil {
		log.Printf("Warning: tracing unavailable: %v", err)
		shutdownTracer = func(context.Context) error { return nil }
	}
	defer func() {
		_ = shutdownTracer(context.Background())
	}()

	// --- Temporal client ---
	tc, err := temporal.NewClient(&temporal.Config{
		HostPort:  getEnv("TEMPORAL_HOST_PORT", "localhost:7233"),
		Namespace: getEnv("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer tc.Close()

	// --- Idempotency store (shared across all activities) ---
	idemStore := idempotency.NewMemoryStore()

	// --- Activities ---
	inventoryClient := infrahttp.NewInventoryClient()
	inventoryActivity := activities.NewInventoryActivity(0.30, inventoryClient, idemStore)
	paymentClient := payment.NewHTTPPaymentClient(getEnv("PAYMENT_SERVICE_URL", "http://localhost:8082"))
	paymentActivity := activities.NewPaymentActivity(paymentClient, idemStore)
	shipmentClient := shipment.NewHTTPShipmentClient(getEnv("SHIPMENT_SERVICE_URL", "http://localhost:8083"))
	shippingActivity := activities.NewShippingActivity(1.0, shipmentClient, idemStore)
	fraudActivity := activities.NewFraudCheckActivity(0.10, idemStore)

	// --- Metrics server (worker exposes /metrics on a separate port) ---
	metricsAddr := getEnv("WORKER_METRICS_ADDR", ":9090")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("Worker metrics listening on %s/metrics", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Printf("Worker metrics server error: %v", err)
		}
	}()

	// --- Worker (with metrics + tracing interceptors) ---
	w := temporal.NewWorker(tc.GetClient(), &temporal.WorkerConfig{
		MaxConcurrentWorkflows:  100,
		MaxConcurrentActivities: 100,
		Interceptors: []interceptor.WorkerInterceptor{
			observability.NewMetricsInterceptor(),
			observability.NewTracingInterceptor(),
		},
	})

	w.RegisterWorkflow(workflows.OrderWorkflow)
	w.RegisterWorkflow(workflows.ShipmentWorkflow)
	log.Println("Registered workflows: OrderWorkflow, ShipmentWorkflow")

	w.RegisterActivity(inventoryActivity.ReserveInventory)
	w.RegisterActivity(inventoryActivity.ReleaseInventory)
	w.RegisterActivity(inventoryActivity.CheckAvailability)

	w.RegisterActivity(paymentActivity.ChargePayment)
	w.RegisterActivity(paymentActivity.RefundPayment)
	w.RegisterActivity(paymentActivity.VerifyPayment)

	w.RegisterActivity(shippingActivity.CreateShipment)
	w.RegisterActivity(shippingActivity.CancelShipment)
	w.RegisterActivity(shippingActivity.TrackShipment)

	w.RegisterActivity(fraudActivity.FraudCheck)

	log.Println("Registered 11 activities")

	if err := w.Start(); err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}
	log.Println("Worker started. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down worker...")
	w.Stop()
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
