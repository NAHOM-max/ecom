package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/domain"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
)

type InventoryActivity struct {
	failureRate      float64
	inventoryService domain.InventoryService
	producer         messaging.EventProducer
	idempotency      idempotency.Store
}

func NewInventoryActivity(failureRate float64, inventoryService domain.InventoryService, producer messaging.EventProducer, store idempotency.Store) *InventoryActivity {
	return &InventoryActivity{failureRate: failureRate, inventoryService: inventoryService, producer: producer, idempotency: store}
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

	// Idempotency key: activity name + stable reservation ID derived from activity ID.
	// The activity ID is stable across retries for the same attempt within a workflow.
	idemKey := "ReserveInventory:" + input.OrderID

	// Check: was this reservation already completed on a previous attempt?
	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached ReserveInventoryResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("ReserveInventory: returning cached result (idempotent)",
				"orderID", input.OrderID)
			return &cached, nil
		}
	}

	logger.Info("ReserveInventory started",
		"orderID", input.OrderID,
		"itemCount", len(input.Items),
		"attempt", info.Attempt)

	totalAmount := 0
	for _, item := range input.Items {
		totalAmount += item.Quantity
	}

	domainItems := make([]domain.ReserveItem, len(input.Items))
	for i, item := range input.Items {
		domainItems[i] = domain.ReserveItem{ProductID: item.ProductID, Quantity: item.Quantity}
	}

	resp, err := a.inventoryService.Reserve(ctx, input.OrderID, domainItems)
	if err != nil {
		// Business failure (non-201 mapped as error) — cache and return without retrying
		if isBusinessFailure(err) {
			result := &ReserveInventoryResult{
				ReservationID: "",
				Success:       false,
				Message:       err.Error(),
			}
			_ = a.idempotency.Save(ctx, idemKey, result)
			return result, nil
		}
		// Network/system error — let Temporal retry
		logger.Error("ReserveInventory failed - service error",
			"orderID", input.OrderID, "attempt", info.Attempt, "error", err)
		return nil, err
	}

	result := &ReserveInventoryResult{
		ReservationID: resp.ReservationID,
		Success:       true,
		Message:       "Inventory reserved successfully",
	}

	// Persist before publishing — if publish fails the result is still safe.
	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("ReserveInventory: failed to save idempotency record", "error", err)
	}

	_ = a.producer.Publish(messaging.TopicInventory, messaging.Event{
		EventID:   fmt.Sprintf("evt-%s", result.ReservationID),
		EventType: messaging.EventInventoryReserved,
		Timestamp: time.Now(),
		OrderID:   input.OrderID,
		Payload: messaging.InventoryReservedPayload{
			ReservationID: result.ReservationID,
			ItemCount:     len(input.Items),
		},
	})

	logger.Info("ReserveInventory completed",
		"orderID", input.OrderID, "reservationID", result.ReservationID)
	return result, nil
}

// isBusinessFailure returns true for errors originating from a non-201 HTTP response.
// These carry the prefix set by InventoryClient and must not be retried.
func isBusinessFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return len(msg) > 0 && (startsWith(msg, "inventory service error:") || startsWith(msg, "inventory client: missing"))
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
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

	if err := a.inventoryService.Release(ctx, reservationID); err != nil {
		logger.Error("ReleaseInventory failed - service error",
			"reservationID", reservationID, "attempt", info.Attempt, "error", err)
		return err
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
