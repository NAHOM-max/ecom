package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	temporalinfra "github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
	httphandler "github.com/yourorg/order-fulfillment-temporal-demo/internal/interfaces/http"
	"github.com/yourorg/order-fulfillment-temporal-demo/platform/observability"
)

func main() {
	// --- Tracing ---
	otlpEndpoint := envOr("OTLP_ENDPOINT", "localhost:4318")
	shutdownTracer, err := observability.InitTracer("order-fulfillment-api", otlpEndpoint)
	if err != nil {
		// Non-fatal: log and continue without tracing
		log.Printf("Warning: tracing unavailable: %v", err)
		shutdownTracer = func(context.Context) error { return nil }
	}

	// --- Temporal client ---
	temporalClient, err := temporalinfra.NewClient(&temporalinfra.Config{
		HostPort:  envOr("TEMPORAL_HOST_PORT", "localhost:7233"),
		Namespace: envOr("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		log.Fatalf("failed to connect to Temporal: %v", err)
	}
	defer temporalClient.Close()

	// --- HTTP server ---
	handler := httphandler.NewOrderHandler(temporalClient)
	router := httphandler.NewRouter(handler)

	srv := &http.Server{
		Addr:    envOr("API_ADDR", ":8081"),
		Handler: router,
	}

	go func() {
		log.Printf("API server listening on %s  (metrics: %s/metrics)", srv.Addr, srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = shutdownTracer(ctx)

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
