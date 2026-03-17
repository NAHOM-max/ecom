package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/temporal"
)

func main() {
	// --- Temporal client ---
	tc, err := temporal.NewClient(&temporal.Config{
		HostPort:  getEnv("TEMPORAL_HOST_PORT", "localhost:7233"),
		Namespace: getEnv("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer tc.Close()

	// --- Event producer ---
	// Use a real Kafka producer when KAFKA_BROKERS is set; fall back to no-op.
	producer := buildProducer()
	defer producer.Close()

	// --- Activities (producer injected) ---
	inventoryActivity := activities.NewInventoryActivity(0.30, producer)
	paymentActivity := activities.NewPaymentActivity(0.30, producer)
	shippingActivity := activities.NewShippingActivity(0.30, producer)

	// --- Worker ---
	w := temporal.NewWorker(tc.GetClient(), &temporal.WorkerConfig{
		MaxConcurrentWorkflows:  100,
		MaxConcurrentActivities: 100,
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

	log.Println("Registered 9 activities")

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

// buildProducer returns a KafkaProducer when KAFKA_BROKERS is set,
// otherwise a NoopProducer so the worker runs without Kafka.
func buildProducer() messaging.EventProducer {
	brokers := getEnv("KAFKA_BROKERS", "")
	if brokers == "" {
		log.Println("KAFKA_BROKERS not set — using no-op event producer")
		return &messaging.NoopProducer{}
	}

	p, err := messaging.NewKafkaProducer(messaging.KafkaConfig{
		Brokers: strings.Split(brokers, ","),
	})
	if err != nil {
		log.Printf("Failed to connect to Kafka (%v) — falling back to no-op producer", err)
		return &messaging.NoopProducer{}
	}
	return p
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
