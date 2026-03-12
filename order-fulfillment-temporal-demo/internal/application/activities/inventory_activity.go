package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"
)

// InventoryActivity simulates inventory service operations
type InventoryActivity struct {
	failureRate float64 // Probability of failure (0.0 to 1.0)
}

// NewInventoryActivity creates a new inventory activity
func NewInventoryActivity(failureRate float64) *InventoryActivity {
	return &InventoryActivity{
		failureRate: failureRate,
	}
}

// ReserveInventoryInput contains parameters for inventory reservation
type ReserveInventoryInput struct {
	OrderID string
	Items   []InventoryItem
}

type InventoryItem struct {
	ProductID string
	Quantity  int
}

// ReserveInventoryResult contains the reservation result
type ReserveInventoryResult struct {
	ReservationID string
	Success       bool
	Message       string
}

// ReserveInventory reserves inventory for an order
// Idempotent - uses activity ID as reservation ID
func (a *InventoryActivity) ReserveInventory(ctx context.Context, input ReserveInventoryInput) (*ReserveInventoryResult, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	// Generate idempotent reservation ID based on activity ID
	reservationID := fmt.Sprintf("res-%s", activityInfo.ActivityID)

	logger.Info("ReserveInventory started",
		"orderID", input.OrderID,
		"reservationID", reservationID,
		"itemCount", len(input.Items),
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("ReserveInventory failed - simulated service error",
			"orderID", input.OrderID,
			"reservationID", reservationID,
			"attempt", activityInfo.Attempt)
		return nil, fmt.Errorf("inventory service unavailable: connection timeout")
	}

	// Simulate checking stock availability
	for _, item := range input.Items {
		logger.Info("Checking stock",
			"productID", item.ProductID,
			"quantity", item.Quantity)

		// Simulate occasional out-of-stock (non-retryable)
		if rand.Float64() < 0.05 { // 5% chance
			logger.Error("Product out of stock",
				"productID", item.ProductID,
				"orderID", input.OrderID)
			return &ReserveInventoryResult{
				ReservationID: "",
				Success:       false,
				Message:       fmt.Sprintf("Product %s is out of stock", item.ProductID),
			}, nil // Return success with failure flag (business error, not retryable)
		}
	}

	logger.Info("ReserveInventory completed successfully",
		"orderID", input.OrderID,
		"reservationID", reservationID,
		"itemCount", len(input.Items))

	return &ReserveInventoryResult{
		ReservationID: reservationID,
		Success:       true,
		Message:       "Inventory reserved successfully",
	}, nil
}

// ReleaseInventory releases previously reserved inventory (compensation)
// Idempotent - safe to call multiple times
func (a *InventoryActivity) ReleaseInventory(ctx context.Context, reservationID string) error {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	logger.Info("ReleaseInventory started",
		"reservationID", reservationID,
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	// Simulate service instability (lower failure rate for compensation)
	if rand.Float64() < (a.failureRate * 0.5) { // Half the failure rate
		logger.Warn("ReleaseInventory failed - simulated service error",
			"reservationID", reservationID,
			"attempt", activityInfo.Attempt)
		return fmt.Errorf("inventory service unavailable: connection timeout")
	}

	// Idempotent: Check if already released (simulate)
	logger.Info("Checking reservation status", "reservationID", reservationID)

	// Simulate releasing inventory
	logger.Info("Releasing inventory", "reservationID", reservationID)

	logger.Info("ReleaseInventory completed successfully",
		"reservationID", reservationID)

	return nil
}

// CheckAvailability checks if items are in stock
func (a *InventoryActivity) CheckAvailability(ctx context.Context, items []InventoryItem) (bool, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	logger.Info("CheckAvailability started",
		"itemCount", len(items),
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(50+rand.Intn(200)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("CheckAvailability failed - simulated service error",
			"attempt", activityInfo.Attempt)
		return false, fmt.Errorf("inventory service unavailable: connection timeout")
	}

	// Simulate checking each item
	for _, item := range items {
		logger.Info("Checking availability",
			"productID", item.ProductID,
			"quantity", item.Quantity)
	}

	logger.Info("CheckAvailability completed successfully", "available", true)
	return true, nil
}
