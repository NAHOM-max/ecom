package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
)

type InventoryActivity struct {
	failureRate float64
	producer    messaging.EventProducer
	idempotency idempotency.Store
}

func NewInventoryActivity(failureRate float64, producer messaging.EventProducer, store idempotency.Store) *InventoryActivity {
	return &InventoryActivity{failureRate: failureRate, producer: producer, idempotency: store}
}

type ReserveInventoryInput struct {
	OrderID string
	Items   []InventoryItem
}

type InventoryItem struct {
	ProductID string
	Quantity  int
}

type ReserveInventoryResult struct {
	ReservationID string
	Success       bool
	Message       string
}

func (a *InventoryActivity) ReserveInventory(ctx context.Context, input ReserveInventoryInput) (*ReserveInventoryResult, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	reservationID := fmt.Sprintf("res-%s", info.ActivityID)
	// Idempotency key: activity name + stable reservation ID derived from activity ID.
	// The activity ID is stable across retries for the same attempt within a workflow.
	idemKey := "ReserveInventory:" + reservationID

	// Check: was this reservation already completed on a previous attempt?
	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached ReserveInventoryResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("ReserveInventory: returning cached result (idempotent)",
				"orderID", input.OrderID, "reservationID", reservationID)
			return &cached, nil
		}
	}

	logger.Info("ReserveInventory started",
		"orderID", input.OrderID,
		"reservationID", reservationID,
		"itemCount", len(input.Items),
		"attempt", info.Attempt)

	time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		logger.Warn("ReserveInventory failed - simulated service error",
			"orderID", input.OrderID, "attempt", info.Attempt)
		return nil, fmt.Errorf("inventory service unavailable: connection timeout")
	}

	for _, item := range input.Items {
		if rand.Float64() < 0.05 {
			result := &ReserveInventoryResult{
				ReservationID: "",
				Success:       false,
				Message:       fmt.Sprintf("Product %s is out of stock", item.ProductID),
			}
			// Business failures are also cached — no point retrying an out-of-stock.
			_ = a.idempotency.Save(ctx, idemKey, result)
			return result, nil
		}
	}

	result := &ReserveInventoryResult{
		ReservationID: reservationID,
		Success:       true,
		Message:       "Inventory reserved successfully",
	}

	// Persist before publishing — if publish fails the result is still safe.
	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("ReserveInventory: failed to save idempotency record", "error", err)
	}

	_ = a.producer.Publish(messaging.TopicInventory, messaging.Event{
		EventID:   fmt.Sprintf("evt-%s", reservationID),
		EventType: messaging.EventInventoryReserved,
		Timestamp: time.Now(),
		OrderID:   input.OrderID,
		Payload: messaging.InventoryReservedPayload{
			ReservationID: reservationID,
			ItemCount:     len(input.Items),
		},
	})

	logger.Info("ReserveInventory completed",
		"orderID", input.OrderID, "reservationID", reservationID)
	return result, nil
}

func (a *InventoryActivity) ReleaseInventory(ctx context.Context, reservationID string) error {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	idemKey := "ReleaseInventory:" + reservationID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		logger.Info("ReleaseInventory: already released (idempotent)", "reservationID", reservationID)
		return nil
	}

	logger.Info("ReleaseInventory started", "reservationID", reservationID, "attempt", info.Attempt)
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	if rand.Float64() < a.failureRate*0.5 {
		return fmt.Errorf("inventory service unavailable: connection timeout")
	}

	if err := a.idempotency.Save(ctx, idemKey, map[string]string{"status": "released"}); err != nil {
		logger.Warn("ReleaseInventory: failed to save idempotency record", "error", err)
	}

	logger.Info("ReleaseInventory completed", "reservationID", reservationID)
	return nil
}

func (a *InventoryActivity) CheckAvailability(ctx context.Context, items []InventoryItem) (bool, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	logger.Info("CheckAvailability started", "itemCount", len(items), "attempt", info.Attempt)
	time.Sleep(time.Duration(50+rand.Intn(200)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		return false, fmt.Errorf("inventory service unavailable: connection timeout")
	}

	logger.Info("CheckAvailability completed", "available", true)
	return true, nil
}
