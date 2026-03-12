package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
)

// OrderWorkflowInput contains the input parameters for order workflow
type OrderWorkflowInput struct {
	OrderID    string
	CustomerID string
	Items      []OrderItemInput
}

type OrderItemInput struct {
	ProductID string
	Quantity  int
	Price     float64
}

// OrderWorkflowResult contains the workflow execution result
type OrderWorkflowResult struct {
	OrderID    string
	Status     string
	PaymentID  string
	ShipmentID string
	Message    string
}

// OrderWorkflowState tracks the current state for persistence
type OrderWorkflowState struct {
	OrderID           string
	Status            string
	InventoryReserved bool
	ReservationID     string
	PaymentCharged    bool
	PaymentID         string
	ShipmentCreated   bool
	ShipmentID        string
	CompletedSteps    []string
	LastUpdated       time.Time
}

// OrderWorkflow orchestrates the complete order fulfillment process with saga pattern
func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OrderWorkflow started", "orderID", input.OrderID, "customerID", input.CustomerID)

	// Initialize workflow state (persisted between steps)
	state := &OrderWorkflowState{
		OrderID:           input.OrderID,
		Status:            "PROCESSING",
		InventoryReserved: false,
		PaymentCharged:    false,
		ShipmentCreated:   false,
		CompletedSteps:    []string{},
		LastUpdated:       workflow.Now(ctx),
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

	// Step 1: Reserve Inventory
	logger.Info("Step 1: Reserving inventory", "orderID", input.OrderID)
	state.Status = "RESERVING_INVENTORY"
	state.LastUpdated = workflow.Now(ctx)

	var reserveResult activities.ReserveInventoryResult
	err := workflow.ExecuteActivity(ctx, "ReserveInventory", activities.ReserveInventoryInput{
		OrderID: input.OrderID,
		Items:   convertToInventoryItems(input.Items),
	}).Get(ctx, &reserveResult)

	if err != nil {
		logger.Error("Failed to reserve inventory", "orderID", input.OrderID, "error", err)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: fmt.Sprintf("Inventory reservation failed: %v", err),
		}, err
	}

	// Check if inventory reservation succeeded (business error)
	if !reserveResult.Success {
		logger.Error("Inventory reservation failed - business error", "orderID", input.OrderID, "message", reserveResult.Message)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: reserveResult.Message,
		}, nil
	}

	state.InventoryReserved = true
	state.ReservationID = reserveResult.ReservationID
	state.CompletedSteps = append(state.CompletedSteps, "inventory_reserved")
	state.LastUpdated = workflow.Now(ctx)
	logger.Info("Inventory reserved successfully", "orderID", input.OrderID, "reservationID", reserveResult.ReservationID)

	// Step 2: Charge Payment
	logger.Info("Step 2: Charging payment", "orderID", input.OrderID)
	state.Status = "CHARGING_PAYMENT"
	state.LastUpdated = workflow.Now(ctx)

	var paymentResult activities.ChargePaymentResult
	err = workflow.ExecuteActivity(ctx, "ChargePayment", activities.ChargePaymentInput{
		OrderID:    input.OrderID,
		CustomerID: input.CustomerID,
		Amount:     calculateTotal(input.Items),
		Currency:   "USD",
	}).Get(ctx, &paymentResult)

	if err != nil {
		logger.Error("Payment failed, executing compensation", "orderID", input.OrderID, "error", err)
		// Compensation: Release inventory
		compensateInventory(ctx, logger, state.ReservationID)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: fmt.Sprintf("Payment failed: %v", err),
		}, err
	}

	// Check if payment succeeded (business error)
	if paymentResult.Status != "charged" {
		logger.Error("Payment declined - business error", "orderID", input.OrderID, "status", paymentResult.Status, "message", paymentResult.Message)
		// Compensation: Release inventory
		compensateInventory(ctx, logger, state.ReservationID)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: paymentResult.Message,
		}, nil
	}

	state.PaymentCharged = true
	state.PaymentID = paymentResult.PaymentID
	state.CompletedSteps = append(state.CompletedSteps, "payment_charged")
	state.LastUpdated = workflow.Now(ctx)
	logger.Info("Payment charged successfully", "orderID", input.OrderID, "paymentID", paymentResult.PaymentID)

	// Step 3: Create Shipment (Child Workflow)
	logger.Info("Step 3: Creating shipment", "orderID", input.OrderID)
	state.Status = "CREATING_SHIPMENT"
	state.LastUpdated = workflow.Now(ctx)

	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowID: input.OrderID + "-shipment",
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	}
	childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

	var shipmentResult ShipmentWorkflowResult
	err = workflow.ExecuteChildWorkflow(childCtx, ShipmentWorkflow, ShipmentWorkflowInput{
		OrderID:        input.OrderID,
		ShippingMethod: "standard",
	}).Get(ctx, &shipmentResult)

	if err != nil {
		logger.Error("Shipment creation failed, executing compensation", "orderID", input.OrderID, "error", err)
		// Compensation: Refund payment and release inventory
		compensatePayment(ctx, logger, state.PaymentID)
		compensateInventory(ctx, logger, state.ReservationID)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: fmt.Sprintf("Shipment creation failed: %v", err),
		}, err
	}

	// Check if shipment succeeded (business error)
	if !shipmentResult.Success {
		logger.Error("Shipment creation failed - business error", "orderID", input.OrderID, "message", shipmentResult.Message)
		// Compensation: Refund payment and release inventory
		compensatePayment(ctx, logger, state.PaymentID)
		compensateInventory(ctx, logger, state.ReservationID)
		state.Status = "FAILED"
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: shipmentResult.Message,
		}, nil
	}

	state.ShipmentCreated = true
	state.ShipmentID = shipmentResult.ShipmentID
	state.CompletedSteps = append(state.CompletedSteps, "shipment_created")
	state.LastUpdated = workflow.Now(ctx)
	logger.Info("Shipment created successfully", "orderID", input.OrderID, "shipmentID", shipmentResult.ShipmentID)

	// Step 4: Complete Order
	logger.Info("Step 4: Completing order", "orderID", input.OrderID)
	state.Status = "COMPLETED"
	state.CompletedSteps = append(state.CompletedSteps, "order_completed")
	state.LastUpdated = workflow.Now(ctx)

	logger.Info("OrderWorkflow completed successfully",
		"orderID", input.OrderID,
		"paymentID", state.PaymentID,
		"shipmentID", state.ShipmentID,
		"completedSteps", state.CompletedSteps)

	return &OrderWorkflowResult{
		OrderID:    input.OrderID,
		Status:     "COMPLETED",
		PaymentID:  state.PaymentID,
		ShipmentID: state.ShipmentID,
		Message:    "Order completed successfully",
	}, nil
}

// compensateInventory releases reserved inventory (saga compensation)
func compensateInventory(ctx workflow.Context, logger log.Logger, reservationID string) {
	logger.Warn("Compensating: Releasing inventory", "reservationID", reservationID)

	// Use separate context for compensation with its own timeout
	compensationOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	compensationCtx := workflow.WithActivityOptions(ctx, compensationOptions)

	err := workflow.ExecuteActivity(compensationCtx, "ReleaseInventory", reservationID).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to release inventory during compensation", "reservationID", reservationID, "error", err)
		// In production, this might trigger an alert or manual intervention
	} else {
		logger.Info("Inventory released successfully", "reservationID", reservationID)
	}
}

// compensatePayment refunds the payment (saga compensation)
func compensatePayment(ctx workflow.Context, logger log.Logger, paymentID string) {
	logger.Warn("Compensating: Refunding payment", "paymentID", paymentID)

	// Use separate context for compensation with its own timeout
	compensationOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	compensationCtx := workflow.WithActivityOptions(ctx, compensationOptions)

	err := workflow.ExecuteActivity(compensationCtx, "RefundPayment", paymentID).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to refund payment during compensation", "paymentID", paymentID, "error", err)
		// In production, this might trigger an alert or manual intervention
	} else {
		logger.Info("Payment refunded successfully", "paymentID", paymentID)
	}
}

// Helper functions

func convertToInventoryItems(items []OrderItemInput) []activities.InventoryItem {
	result := make([]activities.InventoryItem, len(items))
	for i, item := range items {
		result[i] = activities.InventoryItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
		}
	}
	return result
}

func calculateTotal(items []OrderItemInput) float64 {
	total := 0.0
	for _, item := range items {
		total += float64(item.Quantity) * item.Price
	}
	return total
}
