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
	OrderID         string
	CustomerAddress ShippingAddress
	Items           []ShipmentItem
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

type ShipmentItem struct {
	ProductID   string
	Quantity    int
	Weight      float64
	Description string
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

// ShipmentWorkflowState tracks shipment workflow state
type ShipmentWorkflowState struct {
	OrderID           string
	Status            string
	ShipmentCreated   bool
	ShipmentID        string
	TrackingNumber    string
	Carrier           string
	ConfirmationSent  bool
	CompletedSteps    []string
	LastUpdated       time.Time
}

// ShipmentWorkflow orchestrates the shipping process as a child workflow
// Lifecycle:
// 1. Create shipment with carrier
// 2. Wait for simulated shipping confirmation
// 3. Mark shipment as completed
func ShipmentWorkflow(ctx workflow.Context, input ShipmentWorkflowInput) (*ShipmentWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("ShipmentWorkflow started",
		"orderID", input.OrderID,
		"shippingMethod", input.ShippingMethod,
		"destination", fmt.Sprintf("%s, %s", input.CustomerAddress.City, input.CustomerAddress.State))

	// Initialize workflow state
	state := &ShipmentWorkflowState{
		OrderID:          input.OrderID,
		Status:           "CREATING",
		ShipmentCreated:  false,
		ConfirmationSent: false,
		CompletedSteps:   []string{},
		LastUpdated:      workflow.Now(ctx),
	}

	// Configure activity options with retry policy
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

	// Step 1: Create Shipment
	logger.Info("Step 1: Creating shipment", "orderID", input.OrderID)
	state.Status = "CREATING_SHIPMENT"
	state.LastUpdated = workflow.Now(ctx)

	var createResult activities.CreateShipmentResult
	err := workflow.ExecuteActivity(ctx, "CreateShipment", activities.CreateShipmentInput{
		OrderID:         input.OrderID,
		CustomerAddress: convertToShippingAddress(input.CustomerAddress),
		Items:           convertToShippingItems(input.Items),
		ShippingMethod:  input.ShippingMethod,
	}).Get(ctx, &createResult)

	if err != nil {
		logger.Error("Failed to create shipment", "orderID", input.OrderID, "error", err)
		state.Status = "FAILED"
		return &ShipmentWorkflowResult{
			Success: false,
			Message: fmt.Sprintf("Shipment creation failed: %v", err),
		}, err
	}

	// Check if shipment creation succeeded (business error)
	if !createResult.Success {
		logger.Error("Shipment creation failed - business error",
			"orderID", input.OrderID,
			"message", createResult.Message)
		state.Status = "FAILED"
		return &ShipmentWorkflowResult{
			Success: false,
			Message: createResult.Message,
		}, nil
	}

	state.ShipmentCreated = true
	state.ShipmentID = createResult.ShipmentID
	state.TrackingNumber = createResult.TrackingNumber
	state.Carrier = createResult.Carrier
	state.CompletedSteps = append(state.CompletedSteps, "shipment_created")
	state.LastUpdated = workflow.Now(ctx)

	logger.Info("Shipment created successfully",
		"orderID", input.OrderID,
		"shipmentID", createResult.ShipmentID,
		"trackingNumber", createResult.TrackingNumber,
		"carrier", createResult.Carrier)

	// Step 2: Wait for Simulated Shipping Confirmation
	logger.Info("Step 2: Waiting for shipping confirmation",
		"orderID", input.OrderID,
		"shipmentID", state.ShipmentID)
	state.Status = "AWAITING_CONFIRMATION"
	state.LastUpdated = workflow.Now(ctx)

	// Simulate waiting for carrier confirmation (2-5 seconds)
	confirmationDelay := time.Second * 3
	err = workflow.Sleep(ctx, confirmationDelay)
	if err != nil {
		logger.Error("Workflow cancelled during confirmation wait", "error", err)
		return &ShipmentWorkflowResult{
			Success: false,
			Message: "Shipment workflow cancelled",
		}, err
	}

	state.ConfirmationSent = true
	state.CompletedSteps = append(state.CompletedSteps, "confirmation_received")
	state.LastUpdated = workflow.Now(ctx)

	logger.Info("Shipping confirmation received",
		"orderID", input.OrderID,
		"shipmentID", state.ShipmentID,
		"trackingNumber", state.TrackingNumber)

	// Step 3: Mark Shipment Completed
	logger.Info("Step 3: Marking shipment as completed",
		"orderID", input.OrderID,
		"shipmentID", state.ShipmentID)
	state.Status = "COMPLETED"
	state.CompletedSteps = append(state.CompletedSteps, "shipment_completed")
	state.LastUpdated = workflow.Now(ctx)

	logger.Info("ShipmentWorkflow completed successfully",
		"orderID", input.OrderID,
		"shipmentID", state.ShipmentID,
		"trackingNumber", state.TrackingNumber,
		"carrier", state.Carrier,
		"completedSteps", state.CompletedSteps)

	return &ShipmentWorkflowResult{
		ShipmentID:     state.ShipmentID,
		TrackingNumber: state.TrackingNumber,
		Carrier:        state.Carrier,
		EstimatedDate:  createResult.EstimatedDate,
		Success:        true,
		Message:        "Shipment completed successfully",
	}, nil
}

// Helper functions

func convertToShippingAddress(addr ShippingAddress) activities.ShippingAddress {
	return activities.ShippingAddress{
		Name:       addr.Name,
		Street:     addr.Street,
		City:       addr.City,
		State:      addr.State,
		PostalCode: addr.PostalCode,
		Country:    addr.Country,
		Phone:      addr.Phone,
	}
}

func convertToShippingItems(items []ShipmentItem) []activities.ShippingItem {
	result := make([]activities.ShippingItem, len(items))
	for i, item := range items {
		result[i] = activities.ShippingItem{
			ProductID:   item.ProductID,
			Quantity:    item.Quantity,
			Weight:      item.Weight,
			Description: item.Description,
		}
	}
	return result
}
