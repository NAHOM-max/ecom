package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"
)

// ShippingActivity simulates shipping service operations
type ShippingActivity struct {
	failureRate float64 // Probability of failure (0.0 to 1.0)
}

// NewShippingActivity creates a new shipping activity
func NewShippingActivity(failureRate float64) *ShippingActivity {
	return &ShippingActivity{
		failureRate: failureRate,
	}
}

// CreateShipmentInput contains shipment creation parameters
type CreateShipmentInput struct {
	OrderID         string
	CustomerAddress ShippingAddress
	Items           []ShippingItem
	ShippingMethod  string
}

type ShippingAddress struct {
	Name       string
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
	Phone      string
}

type ShippingItem struct {
	ProductID   string
	Quantity    int
	Weight      float64
	Description string
}

// CreateShipmentResult contains shipment creation result
type CreateShipmentResult struct {
	ShipmentID     string
	TrackingNumber string
	Carrier        string
	EstimatedDate  string
	Success        bool
	Message        string
}

// CreateShipment creates a new shipment
// Idempotent - uses activity ID as shipment ID
func (a *ShippingActivity) CreateShipment(ctx context.Context, input CreateShipmentInput) (*CreateShipmentResult, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	// Generate idempotent shipment ID based on activity ID
	shipmentID := fmt.Sprintf("ship-%s", activityInfo.ActivityID)
	activityIDStr := activityInfo.ActivityID
	if len(activityIDStr) > 8 {
		activityIDStr = activityIDStr[:8]
	}
	trackingNumber := fmt.Sprintf("TRK%d%s", time.Now().Unix(), activityIDStr)

	logger.Info("CreateShipment started",
		"orderID", input.OrderID,
		"shipmentID", shipmentID,
		"shippingMethod", input.ShippingMethod,
		"destination", fmt.Sprintf("%s, %s", input.CustomerAddress.City, input.CustomerAddress.State),
		"itemCount", len(input.Items),
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(150+rand.Intn(500)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("CreateShipment failed - simulated service error",
			"orderID", input.OrderID,
			"shipmentID", shipmentID,
			"attempt", activityInfo.Attempt)
		return nil, fmt.Errorf("shipping service unavailable: API timeout")
	}

	// Simulate address validation
	logger.Info("Validating shipping address",
		"city", input.CustomerAddress.City,
		"state", input.CustomerAddress.State,
		"postalCode", input.CustomerAddress.PostalCode)

	// Simulate occasional invalid address (non-retryable)
	if rand.Float64() < 0.02 { // 2% chance
		logger.Error("Invalid shipping address",
			"orderID", input.OrderID,
			"address", fmt.Sprintf("%s, %s", input.CustomerAddress.City, input.CustomerAddress.State))
		return &CreateShipmentResult{
			ShipmentID:     "",
			TrackingNumber: "",
			Carrier:        "",
			EstimatedDate:  "",
			Success:        false,
			Message:        "Invalid shipping address: address not found",
		}, nil // Return success with failure flag (business error)
	}

	// Simulate carrier selection
	carriers := []string{"FedEx", "UPS", "DHL", "USPS"}
	carrier := carriers[rand.Intn(len(carriers))]
	logger.Info("Carrier selected", "carrier", carrier)

	// Simulate calculating weight
	totalWeight := 0.0
	for _, item := range input.Items {
		totalWeight += item.Weight * float64(item.Quantity)
		logger.Info("Processing item",
			"productID", item.ProductID,
			"quantity", item.Quantity,
			"weight", item.Weight)
	}
	logger.Info("Total shipment weight calculated", "weight", totalWeight)

	// Simulate generating shipping label
	logger.Info("Generating shipping label",
		"shipmentID", shipmentID,
		"trackingNumber", trackingNumber)
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// Calculate estimated delivery date
	daysToDeliver := 3 + rand.Intn(5) // 3-7 days
	estimatedDate := time.Now().AddDate(0, 0, daysToDeliver).Format("2006-01-02")

	logger.Info("CreateShipment completed successfully",
		"orderID", input.OrderID,
		"shipmentID", shipmentID,
		"trackingNumber", trackingNumber,
		"carrier", carrier,
		"estimatedDate", estimatedDate)

	return &CreateShipmentResult{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		Carrier:        carrier,
		EstimatedDate:  estimatedDate,
		Success:        true,
		Message:        "Shipment created successfully",
	}, nil
}

// CancelShipment cancels a shipment (compensation)
// Idempotent - safe to call multiple times
func (a *ShippingActivity) CancelShipment(ctx context.Context, shipmentID string) error {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	logger.Info("CancelShipment started",
		"shipmentID", shipmentID,
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)

	// Simulate service instability (lower failure rate for cancellation)
	if rand.Float64() < (a.failureRate * 0.5) { // Half the failure rate
		logger.Warn("CancelShipment failed - simulated service error",
			"shipmentID", shipmentID,
			"attempt", activityInfo.Attempt)
		return fmt.Errorf("shipping service unavailable: API timeout")
	}

	// Idempotent: Check if already cancelled (simulate)
	logger.Info("Checking shipment status", "shipmentID", shipmentID)

	// Simulate cancelling with carrier
	logger.Info("Cancelling shipment with carrier", "shipmentID", shipmentID)

	logger.Info("CancelShipment completed successfully",
		"shipmentID", shipmentID)

	return nil
}

// TrackShipment retrieves current shipment status
func (a *ShippingActivity) TrackShipment(ctx context.Context, trackingNumber string) (string, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	logger.Info("TrackShipment started",
		"trackingNumber", trackingNumber,
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("TrackShipment failed - simulated service error",
			"trackingNumber", trackingNumber,
			"attempt", activityInfo.Attempt)
		return "", fmt.Errorf("shipping service unavailable: connection timeout")
	}

	statuses := []string{"label_created", "picked_up", "in_transit", "out_for_delivery", "delivered"}
	status := statuses[rand.Intn(len(statuses))]

	logger.Info("TrackShipment completed successfully",
		"trackingNumber", trackingNumber,
		"status", status)

	return status, nil
}
