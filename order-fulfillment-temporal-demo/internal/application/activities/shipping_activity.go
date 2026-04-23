package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/shipment"
)

type ShippingActivity struct {
	failureRate    float64
	shipmentClient shipment.ShipmentClient
	idempotency    idempotency.Store
}

func NewShippingActivity(failureRate float64, shipmentClient shipment.ShipmentClient, store idempotency.Store) *ShippingActivity {
	return &ShippingActivity{failureRate: failureRate, shipmentClient: shipmentClient, idempotency: store}
}

type CreateShipmentInput struct {
	OrderID         string
	OrderCreatedAt  time.Time
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

	idemKey := "CreateShipment:" + input.OrderID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached CreateShipmentResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("CreateShipment: returning cached result (idempotent)",
				"orderID", input.OrderID, "shipmentID", cached.ShipmentID)
			return &cached, nil
		}
	}

	logger.Info("CreateShipment started",
		"orderID", input.OrderID,
		"shippingMethod", input.ShippingMethod,
		"attempt", info.Attempt)

	resp, err := a.shipmentClient.CreateShipment(ctx, shipment.CreateShipmentRequest{
		OrderID:        input.OrderID,
		OrderCreatedAt: input.OrderCreatedAt,
		WorkflowID:     info.WorkflowExecution.ID,
		Address: shipment.AddressRequest{
			Name:    input.CustomerAddress.Name,
			Street:  input.CustomerAddress.Street,
			City:    input.CustomerAddress.City,
			Country: input.CustomerAddress.Country,
		},
	})
	if err != nil {
		logger.Error("CreateShipment failed - service error",
			"orderID", input.OrderID, "attempt", info.Attempt, "error", err)
		return nil, err
	}

	estimatedDate := resp.DeliveryDate.Format("2006-01-02")

	result := &CreateShipmentResult{
		ShipmentID:     resp.ID,
		TrackingNumber: resp.TrackingNumber,
		Carrier:        resp.Status,
		EstimatedDate:  estimatedDate,
		Success:        true,
		Message:        "Shipment created successfully",
	}

	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("CreateShipment: failed to save idempotency record", "error", err)
	}

	logger.Info("CreateShipment completed",
		"orderID", input.OrderID, "shipmentID", resp.ID, "trackingNumber", resp.TrackingNumber)
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
