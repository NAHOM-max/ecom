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

type ShippingActivity struct {
	failureRate float64
	producer    messaging.EventProducer
	idempotency idempotency.Store
}

func NewShippingActivity(failureRate float64, producer messaging.EventProducer, store idempotency.Store) *ShippingActivity {
	return &ShippingActivity{failureRate: failureRate, producer: producer, idempotency: store}
}

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

type CreateShipmentResult struct {
	ShipmentID     string
	TrackingNumber string
	Carrier        string
	EstimatedDate  string
	Success        bool
	Message        string
}

func (a *ShippingActivity) CreateShipment(ctx context.Context, input CreateShipmentInput) (*CreateShipmentResult, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	shipmentID := fmt.Sprintf("ship-%s", info.ActivityID)

	idemKey := "CreateShipment:" + input.OrderID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached CreateShipmentResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("CreateShipment: returning cached result (idempotent)",
				"orderID", input.OrderID, "shipmentID", shipmentID)
			return &cached, nil
		}
	}

	actIDStr := info.ActivityID
	if len(actIDStr) > 8 {
		actIDStr = actIDStr[:8]
	}
	trackingNumber := fmt.Sprintf("TRK%d%s", time.Now().Unix(), actIDStr)

	logger.Info("CreateShipment started",
		"orderID", input.OrderID,
		"shipmentID", shipmentID,
		"shippingMethod", input.ShippingMethod,
		"attempt", info.Attempt)

	time.Sleep(time.Duration(150+rand.Intn(500)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		return nil, fmt.Errorf("shipping service unavailable: API timeout")
	}

	// Invalid address — business error, cache it.
	if rand.Float64() < 0.02 {
		result := &CreateShipmentResult{Success: false, Message: "Invalid shipping address: address not found"}
		_ = a.idempotency.Save(ctx, idemKey, result)
		return result, nil
	}

	carriers := []string{"FedEx", "UPS", "DHL", "USPS"}
	carrier := carriers[rand.Intn(len(carriers))]
	estimatedDate := time.Now().AddDate(0, 0, 3+rand.Intn(5)).Format("2006-01-02")

	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	result := &CreateShipmentResult{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		Carrier:        carrier,
		EstimatedDate:  estimatedDate,
		Success:        true,
		Message:        "Shipment created successfully",
	}

	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("CreateShipment: failed to save idempotency record", "error", err)
	}

	_ = a.producer.Publish(messaging.TopicShipments, messaging.Event{
		EventID:   fmt.Sprintf("evt-%s", shipmentID),
		EventType: messaging.EventShipmentCreated,
		Timestamp: time.Now(),
		OrderID:   input.OrderID,
		Payload: messaging.ShipmentCreatedPayload{
			ShipmentID:     shipmentID,
			TrackingNumber: trackingNumber,
			Carrier:        carrier,
			EstimatedDate:  estimatedDate,
		},
	})

	logger.Info("CreateShipment completed",
		"orderID", input.OrderID, "shipmentID", shipmentID, "carrier", carrier)
	return result, nil
}

func (a *ShippingActivity) CancelShipment(ctx context.Context, shipmentID string) error {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	idemKey := "CancelShipment:" + shipmentID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		logger.Info("CancelShipment: already cancelled (idempotent)", "shipmentID", shipmentID)
		return nil
	}

	logger.Info("CancelShipment started", "shipmentID", shipmentID, "attempt", info.Attempt)
	time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)

	if rand.Float64() < a.failureRate*0.5 {
		return fmt.Errorf("shipping service unavailable: API timeout")
	}

	if err := a.idempotency.Save(ctx, idemKey, map[string]string{"status": "cancelled"}); err != nil {
		logger.Warn("CancelShipment: failed to save idempotency record", "error", err)
	}

	logger.Info("CancelShipment completed", "shipmentID", shipmentID)
	return nil
}

func (a *ShippingActivity) TrackShipment(ctx context.Context, trackingNumber string) (string, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	logger.Info("TrackShipment started", "trackingNumber", trackingNumber, "attempt", info.Attempt)
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		return "", fmt.Errorf("shipping service unavailable: connection timeout")
	}

	statuses := []string{"label_created", "picked_up", "in_transit", "out_for_delivery", "delivered"}
	status := statuses[rand.Intn(len(statuses))]

	logger.Info("TrackShipment completed", "trackingNumber", trackingNumber, "status", status)
	return status, nil
}
