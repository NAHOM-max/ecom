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
)

func main() {
	hostPort := envOr("TEMPORAL_HOST_PORT", "localhost:7233")
	namespace := envOr("TEMPORAL_NAMESPACE", "default")
	listenAddr := envOr("API_ADDR", ":8081")

	temporalClient, err := temporalinfra.NewClient(&temporalinfra.Config{
		HostPort:  hostPort,
		Namespace: namespace,
	})
	if err != nil {
		log.Fatalf("failed to connect to Temporal: %v", err)
	}
	defer temporalClient.Close()

	handler := httphandler.NewOrderHandler(temporalClient)
	router := httphandler.NewRouter(handler)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}

	go func() {
		log.Printf("API server listening on %s", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
