package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
)

// ShipmentWorkflowInput contains shipment workflow parameters
type ShipmentWorkflowInput struct {
	OrderID        string
	ShippingMethod string
}

// ShipmentWorkflowResult contains the shipment result
type ShipmentWorkflowResult struct {
	ShipmentID     string
	TrackingNumber string
	Carrier        string
	EstimatedDate  string
	Success        bool
	Message        string
}

// ShipmentWorkflow orchestrates the shipping process
func ShipmentWorkflow(ctx workflow.Context, input ShipmentWorkflowInput) (*ShipmentWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("ShipmentWorkflow started", "orderID", input.OrderID, "shippingMethod", input.ShippingMethod)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Create shipment
	logger.Info("Creating shipment", "orderID", input.OrderID)

	var shipmentResult activities.CreateShipmentResult
	err := workflow.ExecuteActivity(ctx, "CreateShipment", activities.CreateShipmentInput{
		OrderID: input.OrderID,
		CustomerAddress: activities.ShippingAddress{
			Name:       "Customer Name",
			Street:     "123 Main St",
			City:       "New York",
			State:      "NY",
			PostalCode: "10001",
			Country:    "USA",
			Phone:      "555-0100",
		},
		Items: []activities.ShippingItem{
			{ProductID: "prod-1", Quantity: 1, Weight: 2.5, Description: "Product"},
		},
		ShippingMethod: input.ShippingMethod,
	}).Get(ctx, &shipmentResult)

	if err != nil {
		logger.Error("Failed to create shipment", "orderID", input.OrderID, "error", err)
		return &ShipmentWorkflowResult{
			Success: false,
			Message: fmt.Sprintf("Shipment creation failed: %v", err),
		}, err
	}

	// Check if shipment creation succeeded (business error)
	if !shipmentResult.Success {
		logger.Error("Shipment creation failed - business error", "orderID", input.OrderID, "message", shipmentResult.Message)
		return &ShipmentWorkflowResult{
			Success: false,
			Message: shipmentResult.Message,
		}, nil
	}

	logger.Info("ShipmentWorkflow completed successfully",
		"orderID", input.OrderID,
		"shipmentID", shipmentResult.ShipmentID,
		"trackingNumber", shipmentResult.TrackingNumber,
		"carrier", shipmentResult.Carrier)

	return &ShipmentWorkflowResult{
		ShipmentID:     shipmentResult.ShipmentID,
		TrackingNumber: shipmentResult.TrackingNumber,
		Carrier:        shipmentResult.Carrier,
		EstimatedDate:  shipmentResult.EstimatedDate,
		Success:        true,
		Message:        "Shipment created successfully",
	}, nil
}
