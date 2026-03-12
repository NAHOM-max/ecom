package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
)

func main() {
	// Create Temporal client
	tc, err := temporal.NewClient(&temporal.Config{
		HostPort:  getEnv("TEMPORAL_HOST_PORT", "localhost:7233"),
		Namespace: getEnv("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer tc.Close()

	// Create worker
	worker := temporal.NewWorker(tc.GetClient(), &temporal.WorkerConfig{
		MaxConcurrentWorkflows:  100,
		MaxConcurrentActivities: 100,
	})

	// Register workflows
	worker.RegisterWorkflow(workflows.OrderWorkflow)
	worker.RegisterWorkflow(workflows.ShipmentWorkflow)
	log.Println("Registered workflows: OrderWorkflow, ShipmentWorkflow")

	// Create activity instances with 30% failure rate
	inventoryActivity := activities.NewInventoryActivity(0.30)
	paymentActivity := activities.NewPaymentActivity(0.30)
	shippingActivity := activities.NewShippingActivity(0.30)

	// Register inventory activities
	worker.RegisterActivity(inventoryActivity.ReserveInventory)
	worker.RegisterActivity(inventoryActivity.ReleaseInventory)
	worker.RegisterActivity(inventoryActivity.CheckAvailability)

	// Register payment activities
	worker.RegisterActivity(paymentActivity.ChargePayment)
	worker.RegisterActivity(paymentActivity.RefundPayment)
	worker.RegisterActivity(paymentActivity.VerifyPayment)

	// Register shipping activities
	worker.RegisterActivity(shippingActivity.CreateShipment)
	worker.RegisterActivity(shippingActivity.CancelShipment)
	worker.RegisterActivity(shippingActivity.TrackShipment)

	log.Println("Registered 9 activities with 30% simulated failure rate")

	// Start worker
	if err := worker.Start(); err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}

	log.Println("Worker started successfully. Press Ctrl+C to stop.")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Received shutdown signal")
	worker.Stop()
	log.Println("Worker shutdown complete")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
