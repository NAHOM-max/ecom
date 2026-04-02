package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/queries"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/updates"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
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

// OrderStep is the typed enum for workflow step transitions.
// It is the single source of truth for where the workflow currently is.
// state.Status is always derived from this — never set independently.
type OrderStep string

const (
	StepInit             OrderStep = "INIT"
	StepReserveInventory OrderStep = "RESERVE_INVENTORY"
	StepFraudCheck       OrderStep = "FRAUD_CHECK"
	StepChargePayment    OrderStep = "CHARGE_PAYMENT"
	StepCreateShipment   OrderStep = "CREATE_SHIPMENT"
	StepCompleted        OrderStep = "COMPLETED"
	StepFailed           OrderStep = "FAILED"
	StepCancelled        OrderStep = "CANCELLED"
)

// OrderWorkflowState tracks the current state for persistence
type OrderWorkflowState struct {
	OrderID           string
	// CurrentStep is the authoritative step position. Status is derived from it.
	CurrentStep       OrderStep
	// Status mirrors string(CurrentStep) for external query visibility.
	Status            string
	Priority          updates.OrderPriority
	InventoryReserved bool
	ReservationID     string
	PaymentCharged    bool
	PaymentID         string
	ShipmentCreated   bool
	ShipmentID        string
	ShippingAddress   *ShippingAddress
	CancelRequested   bool
	CancelReason      string
	CompletedSteps    []string
	LastUpdated       time.Time
}

// IsTerminal returns true when the workflow has reached a final step.
func (s *OrderWorkflowState) IsTerminal() bool {
	return s.CurrentStep == StepCompleted ||
		s.CurrentStep == StepFailed ||
		s.CurrentStep == StepCancelled
}

// CanTransitionTo returns true when moving from the current step to next is
// a valid forward transition. Not enforced yet — defined for future use.
func (s *OrderWorkflowState) CanTransitionTo(next OrderStep) bool {
	switch s.CurrentStep {
	case StepInit:
		return next == StepReserveInventory
	case StepReserveInventory:
		return next == StepFraudCheck || next == StepChargePayment
	case StepFraudCheck:
		return next == StepChargePayment
	case StepChargePayment:
		return next == StepCreateShipment
	case StepCreateShipment:
		return next == StepCompleted
	default:
		return false
	}
}

// OrderWorkflow orchestrates the complete order fulfillment process with saga pattern
func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OrderWorkflow started", "orderID", input.OrderID, "customerID", input.CustomerID)

	// Initialize workflow state (persisted between steps)
	state := &OrderWorkflowState{
		OrderID:        input.OrderID,
		CurrentStep:    StepInit,
		Status:         string(StepInit),
		Priority:       updates.PriorityNormal,
		CompletedSteps: []string{},
		LastUpdated:    workflow.Now(ctx),
	}

	// Register order_status query handler
	if err := workflow.SetQueryHandler(ctx, queries.OrderStatusQuery, func() (queries.OrderStatusResult, error) {
		return queries.OrderStatusResult{
			OrderID:        state.OrderID,
			CurrentStatus:  state.Status,
			PaymentStatus:  paymentStatus(state),
			ShipmentStatus: shipmentStatus(state),
			Priority:       string(state.Priority),
		}, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register order_status query handler: %w", err)
	}

	// Register set_priority update handler
	// The validator runs before the handler; a non-nil error rejects the update
	// without modifying state, giving the caller a synchronous rejection.
	if err := workflow.SetUpdateHandlerWithOptions(
		ctx,
		updates.SetPriorityUpdate,
		func(ctx workflow.Context, input updates.SetPriorityInput) (updates.SetPriorityResult, error) {
			old := state.Priority
			state.Priority = input.Priority
			state.LastUpdated = workflow.Now(ctx)
			logger.Info("Order priority updated",
				"orderID", state.OrderID,
				"oldPriority", old,
				"newPriority", input.Priority,
				"updatedBy", input.UpdatedBy,
			)
			return updates.SetPriorityResult{
				OrderID:     state.OrderID,
				OldPriority: old,
				NewPriority: input.Priority,
			}, nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context, input updates.SetPriorityInput) error {
				return input.Priority.Validate()
			},
		},
	); err != nil {
		return nil, fmt.Errorf("failed to register set_priority update handler: %w", err)
	}

	// Setup signal channels
	cancelChannel := workflow.GetSignalChannel(ctx, signals.CancelOrderSignal)
	updateAddressChannel := workflow.GetSignalChannel(ctx, signals.UpdateShippingAddressSignal)

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
	setStep(state, StepReserveInventory, ctx)

	var reserveResult activities.ReserveInventoryResult

	future := workflow.ExecuteActivity(ctx, "ReserveInventory", activities.ReserveInventoryInput{
		OrderID: input.OrderID,
		Items:   convertToInventoryItems(input.Items),
	})

	err := executeActivityWithSelector(
		ctx,
		cancelChannel,
		updateAddressChannel,
		state,
		logger,
		future,
	)

	if err == nil && !state.CancelRequested {
		// Now safely get result
		err = future.Get(ctx, &reserveResult)
	}

	if err != nil {
		logger.Error("Failed to reserve inventory", "orderID", input.OrderID, "error", err)
		setStep(state, StepFailed, ctx)
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: fmt.Sprintf("Inventory reservation failed: %v", err),
		}, err
	}

	// Check if cancelled during activity
	if state.CancelRequested {
		logger.Warn("Order cancelled during inventory reservation", "orderID", input.OrderID)
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "CANCELLED",
			Message: state.CancelReason,
		}, nil
	}

	// Check if inventory reservation succeeded (business error)
	if !reserveResult.Success {
		logger.Error("Inventory reservation failed - business error", "orderID", input.OrderID, "message", reserveResult.Message)
		setStep(state, StepFailed, ctx)
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

	// -------------------------------------------------------------------------
	// VERSIONING: fraud-check-between-inventory-and-payment
	//
	// workflow.GetVersion records a version marker in the workflow history the
	// first time it is called for a given changeID. On replay it reads the
	// recorded value, so:
	//
	//   • Executions started BEFORE this code was deployed have no marker →
	//     Temporal returns workflow.DefaultVersion (-1). They skip the fraud
	//     check and continue with the original v1 flow.
	//
	//   • Executions started AFTER this code was deployed record version 1 →
	//     They run the fraud check (v2 flow).
	//
	// minSupported = workflow.DefaultVersion allows the worker to replay old
	// histories that pre-date this GetVersion call entirely.
	// maxSupported = OrderWorkflowV2FraudCheck (1) is what new executions record.
	// -------------------------------------------------------------------------
	v := workflow.GetVersion(
		ctx,
		OrderWorkflowChangeIDFraudCheck,
		workflow.DefaultVersion,   // min: still handle pre-versioning histories
		OrderWorkflowV2FraudCheck, // max: current latest version
	)

	if v >= OrderWorkflowV2FraudCheck {
		// ---- V2 path: run fraud check before charging payment ----
		logger.Info("Step 2 (v2): Running fraud check", "orderID", input.OrderID)
		setStep(state, StepFraudCheck, ctx)

		var fraudResult activities.FraudCheckResult

		future := workflow.ExecuteActivity(ctx, "FraudCheck", activities.FraudCheckInput{
			OrderID:    input.OrderID,
			CustomerID: input.CustomerID,
			Amount:     calculateTotal(input.Items),
			Currency:   "USD",
		})

		fraudErr := executeActivityWithSelector(
			ctx,
			cancelChannel,
			updateAddressChannel,
			state,
			logger,
			future,
		)

		if err == nil && !state.CancelRequested {
			// Now safely get result
			err = future.Get(ctx, &fraudResult)
		}

		if fraudErr != nil {
			logger.Error("Fraud check failed, compensating inventory", "orderID", input.OrderID, "error", fraudErr)
			compensateInventory(ctx, logger, state.ReservationID)
			setStep(state, StepFailed, ctx)
			return &OrderWorkflowResult{
				OrderID: input.OrderID,
				Status:  "FAILED",
				Message: fmt.Sprintf("Fraud check error: %v", fraudErr),
			}, fraudErr
		}

		if state.CancelRequested {
			compensateInventory(ctx, logger, state.ReservationID)
			return &OrderWorkflowResult{
				OrderID: input.OrderID,
				Status:  "CANCELLED",
				Message: state.CancelReason,
			}, nil
		}

		if !fraudResult.Approved {
			logger.Warn("Order rejected by fraud check",
				"orderID", input.OrderID,
				"riskScore", fraudResult.RiskScore,
				"reason", fraudResult.Reason,
			)
			compensateInventory(ctx, logger, state.ReservationID)
			setStep(state, StepFailed, ctx)
			return &OrderWorkflowResult{
				OrderID: input.OrderID,
				Status:  "FAILED",
				Message: fmt.Sprintf("Order rejected: %s", fraudResult.Reason),
			}, nil
		}

		state.CompletedSteps = append(state.CompletedSteps, "fraud_check_passed")
		state.LastUpdated = workflow.Now(ctx)
		logger.Info("Fraud check passed",
			"orderID", input.OrderID,
			"riskScore", fraudResult.RiskScore,
		)
	} else {
		// ---- V1 path: no fraud check (replaying old histories) ----
		logger.Info("Step 2 (v1): Skipping fraud check (pre-v2 execution)", "orderID", input.OrderID)
	}

	// Step 2/3: Charge Payment (step number shifts in v2 but logic is identical)
	logger.Info("Charging payment", "orderID", input.OrderID)
	setStep(state, StepChargePayment, ctx)

	var paymentResult activities.ChargePaymentResult

	future = workflow.ExecuteActivity(ctx, "ChargePayment", activities.ChargePaymentInput{
		OrderID:    input.OrderID,
		CustomerID: input.CustomerID,
		Amount:     calculateTotal(input.Items),
		Currency:   "USD",
	})

	err = executeActivityWithSelector(
		ctx,
		cancelChannel,
		updateAddressChannel,
		state,
		logger,
		future,
	)

	if err == nil && !state.CancelRequested {
		// Now safely get result
		err = future.Get(ctx, &paymentResult)
	}

	if err != nil {
		logger.Error("Payment failed, executing compensation", "orderID", input.OrderID, "error", err)
		// Compensation: Release inventory
		compensateInventory(ctx, logger, state.ReservationID)
		setStep(state, StepFailed, ctx)
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "FAILED",
			Message: fmt.Sprintf("Payment failed: %v", err),
		}, err
	}

	// Check if cancelled during payment
	if state.CancelRequested {
		logger.Warn("Order cancelled during payment, compensating", "orderID", input.OrderID)
		compensateInventory(ctx, logger, state.ReservationID)
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "CANCELLED",
			Message: state.CancelReason,
		}, nil
	}

	// Check if payment succeeded (business error)
	if paymentResult.Status != "charged" {
		logger.Error("Payment declined - business error", "orderID", input.OrderID, "status", paymentResult.Status, "message", paymentResult.Message)
		// Compensation: Release inventory
		compensateInventory(ctx, logger, state.ReservationID)
		setStep(state, StepFailed, ctx)
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

	// Step 3: Create Shipment (Child Workflow) - Refactored with Selector
	logger.Info("Step 3: Creating shipment", "orderID", input.OrderID)
	setStep(state, StepCreateShipment, ctx)

	// Use updated shipping address if available
	shippingAddress := ShippingAddress{
		Name:       "Customer Name",
		Street:     "123 Main St",
		City:       "New York",
		State:      "NY",
		PostalCode: "10001",
		Country:    "USA",
		Phone:      "555-0100",
	}
	if state.ShippingAddress != nil {
		shippingAddress = *state.ShippingAddress
		logger.Info("Using updated shipping address", "orderID", input.OrderID, "city", shippingAddress.City)
	}

	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowID: input.OrderID + "-shipment",
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	}
	childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

	// Create cancelable context for child workflow
	ctxWithCancel, cancelHandler := workflow.WithCancel(childCtx)

	// Start child workflow without blocking
	var shipmentResult ShipmentWorkflowResult
	childFuture := workflow.ExecuteChildWorkflow(ctxWithCancel, ShipmentWorkflow, ShipmentWorkflowInput{
		OrderID:         input.OrderID,
		CustomerAddress: shippingAddress,
		Items: []ShipmentItem{
			{ProductID: "prod-1", Quantity: 1, Weight: 2.5, Description: "Product"},
		},
		ShippingMethod: "standard",
	})

	// Create selector to handle child workflow completion, cancel signal, or address update
	selector := workflow.NewSelector(ctx)

	shipmentDone := false

	// CASE 1: Child workflow completion
	selector.AddFuture(childFuture, func(f workflow.Future) {
		err = f.Get(ctx, &shipmentResult)
		shipmentDone = true
	})

	// CASE 2: Cancel signal
	selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
		var cancelRequest signals.CancelOrderRequest
		c.Receive(ctx, &cancelRequest)

		state.CancelRequested = true
		state.CancelReason = fmt.Sprintf(
			"Order cancelled: %s (by %s)",
			cancelRequest.Reason,
			cancelRequest.RequestBy,
		)

		logger.Warn("Cancel signal received during shipment",
			"reason", cancelRequest.Reason,
			"requestBy", cancelRequest.RequestBy)

		// Cancel the child workflow immediately
		cancelHandler()
		err = temporal.NewCanceledError("order cancelled")
	})

	// CASE 3: Address update signal — drain all pending updates, keep the latest
	selector.AddReceive(updateAddressChannel, func(c workflow.ReceiveChannel, more bool) {
		var updateRequest signals.UpdateShippingAddressRequest
		for c.ReceiveAsync(&updateRequest) {
			state.ShippingAddress = &ShippingAddress{
				Name:       updateRequest.Name,
				Street:     updateRequest.Street,
				City:       updateRequest.City,
				State:      updateRequest.State,
				PostalCode: updateRequest.PostalCode,
				Country:    updateRequest.Country,
				Phone:      updateRequest.Phone,
			}
			state.LastUpdated = workflow.Now(ctx)

			logger.Info("Shipping address updated during activity",
				"city", updateRequest.City,
				"state", updateRequest.State)
		}
	})

	// Loop until child workflow completes or cancel signal is received.
	// Address updates re-arm the selector so they are never left unhandled.
	for !shipmentDone && !state.CancelRequested {
		selector.Select(ctx)
	}

	// Handle cancellation
	if state.CancelRequested {
		logger.Warn("Order cancelled during shipment, compensating payment and inventory", "orderID", input.OrderID)
		compensatePayment(ctx, logger, state.PaymentID)
		compensateInventory(ctx, logger, state.ReservationID)
		setStep(state, StepCancelled, ctx)
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  "CANCELLED",
			Message: state.CancelReason,
		}, nil
	}

	// Handle shipment errors
	if err != nil {
		logger.Error("Shipment creation failed, executing compensation", "orderID", input.OrderID, "error", err)
		// Compensation: Refund payment and release inventory
		compensatePayment(ctx, logger, state.PaymentID)
		compensateInventory(ctx, logger, state.ReservationID)
		setStep(state, StepFailed, ctx)
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
		setStep(state, StepFailed, ctx)
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
	setStep(state, StepCompleted, ctx)
	state.CompletedSteps = append(state.CompletedSteps, "order_completed")

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

	// Use disconnected context to ensure compensation runs even if workflow is cancelled
	compCtx, _ := workflow.NewDisconnectedContext(ctx)

	compensationOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	compCtx = workflow.WithActivityOptions(compCtx, compensationOptions)

	err := workflow.ExecuteActivity(compCtx, "ReleaseInventory", reservationID).Get(compCtx, nil)
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

	// Use disconnected context to ensure compensation runs even if workflow is cancelled
	compCtx, _ := workflow.NewDisconnectedContext(ctx)

	compensationOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	compCtx = workflow.WithActivityOptions(compCtx, compensationOptions)

	err := workflow.ExecuteActivity(compCtx, "RefundPayment", paymentID).Get(compCtx, nil)
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

// setStep is the single place that advances workflow state.
// It sets CurrentStep (the source of truth), derives Status from it,
// and stamps LastUpdated — so no call site needs to do any of these manually.
func setStep(state *OrderWorkflowState, step OrderStep, ctx workflow.Context) {
	state.CurrentStep = step
	state.Status = string(step)
	state.LastUpdated = workflow.Now(ctx)
}

// paymentStatus derives a readable payment status from workflow state.
// Uses CurrentStep instead of the raw Status string.
func paymentStatus(state *OrderWorkflowState) string {
	switch {
	case state.CancelRequested && state.PaymentCharged:
		return "refunded"
	case state.PaymentCharged:
		return "charged"
	case state.CurrentStep == StepChargePayment:
		return "pending"
	default:
		return "not_started"
	}
}

// shipmentStatus derives a readable shipment status from workflow state.
// Uses CurrentStep instead of the raw Status string.
func shipmentStatus(state *OrderWorkflowState) string {
	switch {
	case state.CancelRequested && state.ShipmentCreated:
		return "cancelled"
	case state.ShipmentCreated:
		return "created"
	case state.CurrentStep == StepCreateShipment:
		return "pending"
	default:
		return "not_started"
	}
}

// publishEvent fires a PublishEvent activity with best-effort retry.
// Failures are logged but never block the workflow — event publishing
// is a side-effect, not a saga step.
func publishEvent(ctx workflow.Context, logger log.Logger, topic string, event messaging.Event) {
	pubCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	})
	if err := workflow.ExecuteActivity(pubCtx, "Publish",
		activities.PublishEventInput{Topic: topic, Event: event},
	).Get(pubCtx, nil); err != nil {
		logger.Warn("Failed to publish event (non-fatal)",
			"eventType", event.EventType,
			"orderID", event.OrderID,
			"error", err,
		)
	}
}

func executeActivityWithSelector(
	ctx workflow.Context,
	cancelChannel workflow.ReceiveChannel,
	updateAddressChannel workflow.ReceiveChannel,
	state *OrderWorkflowState,
	logger log.Logger,
	activityFuture workflow.Future,
) error {

	selector := workflow.NewSelector(ctx)

	_, actCancel := workflow.WithCancel(ctx)

	var err error
	done := false

	// Activity completion
	selector.AddFuture(activityFuture, func(f workflow.Future) {
		err = f.Get(ctx, nil)
		done = true
	})

	// Cancel signal
	selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
		var cancelRequest signals.CancelOrderRequest
		c.Receive(ctx, &cancelRequest)

		state.CancelRequested = true
		state.CancelReason = fmt.Sprintf(
			"Order cancelled: %s (by %s)",
			cancelRequest.Reason,
			cancelRequest.RequestBy,
		)

		logger.Warn("Cancel signal received during activity",
			"reason", cancelRequest.Reason,
			"requestBy", cancelRequest.RequestBy,
		)

		actCancel()
		done = true
	})

	// Address update signal (🔥 replaces your snippet)
	selector.AddReceive(updateAddressChannel, func(c workflow.ReceiveChannel, more bool) {
		var updateRequest signals.UpdateShippingAddressRequest
		for c.ReceiveAsync(&updateRequest) {
			state.ShippingAddress = &ShippingAddress{
				Name:       updateRequest.Name,
				Street:     updateRequest.Street,
				City:       updateRequest.City,
				State:      updateRequest.State,
				PostalCode: updateRequest.PostalCode,
				Country:    updateRequest.Country,
				Phone:      updateRequest.Phone,
			}
			state.LastUpdated = workflow.Now(ctx)

			logger.Info("Shipping address updated during activity",
				"city", updateRequest.City,
				"state", updateRequest.State)
		}
	})

	// Main loop
	for !done && !state.CancelRequested {
		selector.Select(ctx)
	}

	return err
}
