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
	OrderID     string
	// CurrentStep is the authoritative step position. Status is derived from it.
	CurrentStep OrderStep
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

// OrderWorkflow orchestrates the complete order fulfillment process.
//
// Architecture: single event-driven selector loop.
//
//   INIT
//     → RESERVE_INVENTORY  (activity)
//     → FRAUD_CHECK        (activity, v2 only — gated by GetVersion)
//     → CHARGE_PAYMENT     (activity)
//     → CREATE_SHIPMENT    (child workflow)
//     → COMPLETED
//
// Cancellation and address updates are handled by global signal receivers
// registered once before the loop. The loop itself drives compensation and
// terminal transitions — no blocking future.Get() calls outside selectors.
func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OrderWorkflow started", "orderID", input.OrderID, "customerID", input.CustomerID)

	// -------------------------------------------------------------------------
	// State initialisation
	// -------------------------------------------------------------------------
	state := &OrderWorkflowState{
		OrderID:        input.OrderID,
		CurrentStep:    StepInit,
		Status:         string(StepInit),
		Priority:       updates.PriorityNormal,
		CompletedSteps: []string{},
		LastUpdated:    workflow.Now(ctx),
	}

	// -------------------------------------------------------------------------
	// Query handler — reads derived state, never blocks
	// -------------------------------------------------------------------------
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

	// -------------------------------------------------------------------------
	// Update handler — set_priority
	// -------------------------------------------------------------------------
	if err := workflow.SetUpdateHandlerWithOptions(
		ctx,
		updates.SetPriorityUpdate,
		func(ctx workflow.Context, inp updates.SetPriorityInput) (updates.SetPriorityResult, error) {
			old := state.Priority
			state.Priority = inp.Priority
			state.LastUpdated = workflow.Now(ctx)
			logger.Info("Order priority updated",
				"orderID", state.OrderID,
				"oldPriority", old,
				"newPriority", inp.Priority,
				"updatedBy", inp.UpdatedBy,
			)
			return updates.SetPriorityResult{
				OrderID:     state.OrderID,
				OldPriority: old,
				NewPriority: inp.Priority,
			}, nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context, inp updates.SetPriorityInput) error {
				return inp.Priority.Validate()
			},
		},
	); err != nil {
		return nil, fmt.Errorf("failed to register set_priority update handler: %w", err)
	}

	// -------------------------------------------------------------------------
	// Signal channels
	// -------------------------------------------------------------------------
	cancelChannel := workflow.GetSignalChannel(ctx, signals.CancelOrderSignal)
	updateAddressChannel := workflow.GetSignalChannel(ctx, signals.UpdateShippingAddressSignal)

	// -------------------------------------------------------------------------
	// Activity options (shared across all steps)
	// -------------------------------------------------------------------------
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

	// -------------------------------------------------------------------------
	// Versioning — called ONCE before the loop so the marker is recorded
	// deterministically regardless of which branch the loop takes.
	//
	//   DefaultVersion (-1) → v1 flow: no fraud check
	//   OrderWorkflowV2FraudCheck (1) → v2 flow: fraud check before payment
	// -------------------------------------------------------------------------
	fraudCheckVersion := workflow.GetVersion(
		ctx,
		OrderWorkflowChangeIDFraudCheck,
		workflow.DefaultVersion,
		OrderWorkflowV2FraudCheck,
	)

	// -------------------------------------------------------------------------
	// Loop control variables
	// -------------------------------------------------------------------------
	selector := workflow.NewSelector(ctx)

	var (
		activityRunning bool
		activityCancel  workflow.CancelFunc // cancels the running activity context
		childRunning    bool
		childCancel     workflow.CancelFunc // cancels the child workflow context
	)

	// -------------------------------------------------------------------------
	// Global signal handlers — registered ONCE, re-armed automatically by
	// Temporal's selector on every new signal delivery.
	//
	// A. Cancel signal
	//    Sets CancelRequested, cancels any in-flight work, then lets the loop
	//    body handle compensation and the StepCancelled transition.
	// -------------------------------------------------------------------------
	selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, _ bool) {
		var req signals.CancelOrderRequest
		c.Receive(ctx, &req)

		if state.CancelRequested {
			return // already handling a cancellation
		}

		state.CancelRequested = true
		state.CancelReason = fmt.Sprintf("Order cancelled: %s (by %s)", req.Reason, req.RequestBy)

		logger.Warn("Cancel signal received",
			"orderID", input.OrderID,
			"reason", req.Reason,
			"requestBy", req.RequestBy,
		)

		if activityRunning && activityCancel != nil {
			activityCancel()
		}
		if childRunning && childCancel != nil {
			childCancel()
		}
	})

	// B. Address update signal
	//    Drains all buffered updates, keeps the latest.
	//    Ignored once cancellation is in progress.
	selector.AddReceive(updateAddressChannel, func(c workflow.ReceiveChannel, _ bool) {
		if state.CancelRequested {
			// drain without applying
			var discard signals.UpdateShippingAddressRequest
			for c.ReceiveAsync(&discard) {
			}
			return
		}
		var req signals.UpdateShippingAddressRequest
		for c.ReceiveAsync(&req) {
			state.ShippingAddress = &ShippingAddress{
				Name:       req.Name,
				Street:     req.Street,
				City:       req.City,
				State:      req.State,
				PostalCode: req.PostalCode,
				Country:    req.Country,
				Phone:      req.Phone,
			}
			state.LastUpdated = workflow.Now(ctx)
			logger.Info("Shipping address updated",
				"orderID", input.OrderID,
				"city", req.City,
				"state", req.State,
			)
		}
	})

	// -------------------------------------------------------------------------
	// Advance to first real step
	// -------------------------------------------------------------------------
	setStep(state, StepReserveInventory, ctx)

	// -------------------------------------------------------------------------
	// Main event-driven loop
	//
	// Each iteration either:
	//   (a) starts the next activity/child workflow (if nothing is running), or
	//   (b) waits for the next event via selector.Select.
	//
	// Activity/child futures register their completion callbacks inline when
	// they are started. Signal callbacks are registered once above and
	// re-armed automatically.
	// -------------------------------------------------------------------------
	for !state.IsTerminal() {

		// -- Launch work for the current step (only when nothing is running) --
		if !activityRunning && !childRunning {
			switch state.CurrentStep {

			// -----------------------------------------------------------------
			// STEP: Reserve Inventory
			// -----------------------------------------------------------------
			case StepReserveInventory:
				logger.Info("Starting ReserveInventory", "orderID", input.OrderID)

				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				future := workflow.ExecuteActivity(actCtx, "ReserveInventory",
					activities.ReserveInventoryInput{
						OrderID: input.OrderID,
						Items:   convertToInventoryItems(input.Items),
					})

				selector.AddFuture(future, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					if state.CancelRequested {
						// loop will handle compensation
						return
					}

					var result activities.ReserveInventoryResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ReserveInventory failed", "orderID", input.OrderID, "error", err)
						setStep(state, StepFailed, ctx)
						state.CompletedSteps = append(state.CompletedSteps,
							fmt.Sprintf("reserve_inventory_failed: %v", err))
						return
					}

					if !result.Success {
						logger.Error("ReserveInventory business error",
							"orderID", input.OrderID, "message", result.Message)
						setStep(state, StepFailed, ctx)
						state.CompletedSteps = append(state.CompletedSteps,
							fmt.Sprintf("reserve_inventory_failed: %s", result.Message))
						return
					}

					state.InventoryReserved = true
					state.ReservationID = result.ReservationID
					state.CompletedSteps = append(state.CompletedSteps, "inventory_reserved")
					logger.Info("ReserveInventory succeeded",
						"orderID", input.OrderID, "reservationID", result.ReservationID)

					// Advance: v2 → fraud check, v1 → payment
					if fraudCheckVersion >= OrderWorkflowV2FraudCheck {
						setStep(state, StepFraudCheck, ctx)
					} else {
						setStep(state, StepChargePayment, ctx)
					}
				})

			// -----------------------------------------------------------------
			// STEP: Fraud Check (v2 only)
			// -----------------------------------------------------------------
			case StepFraudCheck:
				logger.Info("Starting FraudCheck", "orderID", input.OrderID)

				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				future := workflow.ExecuteActivity(actCtx, "FraudCheck",
					activities.FraudCheckInput{
						OrderID:    input.OrderID,
						CustomerID: input.CustomerID,
						Amount:     calculateTotal(input.Items),
						Currency:   "USD",
					})

				selector.AddFuture(future, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					if state.CancelRequested {
						return
					}

					var result activities.FraudCheckResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("FraudCheck failed", "orderID", input.OrderID, "error", err)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						return
					}

					if !result.Approved {
						logger.Warn("FraudCheck rejected order",
							"orderID", input.OrderID,
							"riskScore", result.RiskScore,
							"reason", result.Reason,
						)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						state.CompletedSteps = append(state.CompletedSteps,
							fmt.Sprintf("fraud_check_rejected: %s", result.Reason))
						return
					}

					state.CompletedSteps = append(state.CompletedSteps, "fraud_check_passed")
					logger.Info("FraudCheck passed",
						"orderID", input.OrderID, "riskScore", result.RiskScore)
					setStep(state, StepChargePayment, ctx)
				})

			// -----------------------------------------------------------------
			// STEP: Charge Payment
			// -----------------------------------------------------------------
			case StepChargePayment:
				logger.Info("Starting ChargePayment", "orderID", input.OrderID)

				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				future := workflow.ExecuteActivity(actCtx, "ChargePayment",
					activities.ChargePaymentInput{
						OrderID:    input.OrderID,
						CustomerID: input.CustomerID,
						Amount:     calculateTotal(input.Items),
						Currency:   "USD",
					})

				selector.AddFuture(future, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					if state.CancelRequested {
						return
					}

					var result activities.ChargePaymentResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ChargePayment failed", "orderID", input.OrderID, "error", err)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						return
					}

					if result.Status != "charged" {
						logger.Error("ChargePayment declined",
							"orderID", input.OrderID,
							"status", result.Status,
							"message", result.Message,
						)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						state.CompletedSteps = append(state.CompletedSteps,
							fmt.Sprintf("payment_declined: %s", result.Message))
						return
					}

					state.PaymentCharged = true
					state.PaymentID = result.PaymentID
					state.CompletedSteps = append(state.CompletedSteps, "payment_charged")
					logger.Info("ChargePayment succeeded",
						"orderID", input.OrderID, "paymentID", result.PaymentID)
					setStep(state, StepCreateShipment, ctx)
				})

			// -----------------------------------------------------------------
			// STEP: Create Shipment (child workflow)
			// -----------------------------------------------------------------
			case StepCreateShipment:
				logger.Info("Starting ShipmentWorkflow", "orderID", input.OrderID)

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
					logger.Info("Using updated shipping address",
						"orderID", input.OrderID, "city", shippingAddress.City)
				}

				childCtx, cancel := workflow.WithCancel(
					workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
						WorkflowID: input.OrderID + "-shipment",
						RetryPolicy: &temporal.RetryPolicy{
							InitialInterval:    time.Second * 2,
							BackoffCoefficient: 2.0,
							MaximumAttempts:    3,
						},
					}),
				)
				childCancel = cancel
				childRunning = true

				childFuture := workflow.ExecuteChildWorkflow(childCtx, ShipmentWorkflow,
					ShipmentWorkflowInput{
						OrderID:         input.OrderID,
						CustomerAddress: shippingAddress,
						Items: []ShipmentItem{
							{ProductID: "prod-1", Quantity: 1, Weight: 2.5, Description: "Product"},
						},
						ShippingMethod: "standard",
					})

				selector.AddFuture(childFuture, func(f workflow.Future) {
					childRunning = false
					childCancel = nil

					if state.CancelRequested {
						return
					}

					var result ShipmentWorkflowResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ShipmentWorkflow failed",
							"orderID", input.OrderID, "error", err)
						compensatePayment(ctx, logger, state.PaymentID)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						return
					}

					if !result.Success {
						logger.Error("ShipmentWorkflow business error",
							"orderID", input.OrderID, "message", result.Message)
						compensatePayment(ctx, logger, state.PaymentID)
						compensateInventory(ctx, logger, state.ReservationID)
						setStep(state, StepFailed, ctx)
						state.CompletedSteps = append(state.CompletedSteps,
							fmt.Sprintf("shipment_failed: %s", result.Message))
						return
					}

					state.ShipmentCreated = true
					state.ShipmentID = result.ShipmentID
					state.CompletedSteps = append(state.CompletedSteps, "shipment_created")
					logger.Info("ShipmentWorkflow succeeded",
						"orderID", input.OrderID, "shipmentID", result.ShipmentID)
					setStep(state, StepCompleted, ctx)
					state.CompletedSteps = append(state.CompletedSteps, "order_completed")
				})

			// -----------------------------------------------------------------
			// STEP: Init — should never be reached inside the loop;
			// the step is advanced to StepReserveInventory before entering.
			// -----------------------------------------------------------------
			case StepInit:
				// defensive: advance and let the loop restart
				setStep(state, StepReserveInventory, ctx)
			}
		}

		// -- Wait for the next event (activity done, signal, child done) ------
		selector.Select(ctx)

		// -- Centralised cancellation handling --------------------------------
		// The signal handler already cancelled in-flight work.
		// Here we run compensation once nothing is running and transition to
		// StepCancelled so IsTerminal() exits the loop.
		if state.CancelRequested && !activityRunning && !childRunning && !state.IsTerminal() {
			logger.Warn("Handling cancellation",
				"orderID", input.OrderID,
				"reason", state.CancelReason,
			)
			if state.PaymentCharged {
				compensatePayment(ctx, logger, state.PaymentID)
			}
			if state.InventoryReserved {
				compensateInventory(ctx, logger, state.ReservationID)
			}
			setStep(state, StepCancelled, ctx)
		}
	}

	// -------------------------------------------------------------------------
	// Build result from terminal state
	// -------------------------------------------------------------------------
	logger.Info("OrderWorkflow finished",
		"orderID", input.OrderID,
		"step", state.CurrentStep,
		"completedSteps", state.CompletedSteps,
	)

	switch state.CurrentStep {
	case StepCompleted:
		return &OrderWorkflowResult{
			OrderID:    input.OrderID,
			Status:     string(StepCompleted),
			PaymentID:  state.PaymentID,
			ShipmentID: state.ShipmentID,
			Message:    "Order completed successfully",
		}, nil

	case StepCancelled:
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  string(StepCancelled),
			Message: state.CancelReason,
		}, nil

	default: // StepFailed
		msg := "Order failed"
		if len(state.CompletedSteps) > 0 {
			msg = state.CompletedSteps[len(state.CompletedSteps)-1]
		}
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  string(StepFailed),
			Message: msg,
		}, nil
	}
}

// compensateInventory releases reserved inventory (saga compensation).
// Uses a disconnected context so it runs even after workflow cancellation.
func compensateInventory(ctx workflow.Context, logger log.Logger, reservationID string) {
	logger.Warn("Compensating: Releasing inventory", "reservationID", reservationID)

	compCtx, _ := workflow.NewDisconnectedContext(ctx)
	compCtx = workflow.WithActivityOptions(compCtx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	})

	if err := workflow.ExecuteActivity(compCtx, "ReleaseInventory", reservationID).Get(compCtx, nil); err != nil {
		logger.Error("Failed to release inventory", "reservationID", reservationID, "error", err)
	} else {
		logger.Info("Inventory released", "reservationID", reservationID)
	}
}

// compensatePayment refunds the payment (saga compensation).
// Uses a disconnected context so it runs even after workflow cancellation.
func compensatePayment(ctx workflow.Context, logger log.Logger, paymentID string) {
	logger.Warn("Compensating: Refunding payment", "paymentID", paymentID)

	compCtx, _ := workflow.NewDisconnectedContext(ctx)
	compCtx = workflow.WithActivityOptions(compCtx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	})

	if err := workflow.ExecuteActivity(compCtx, "RefundPayment", paymentID).Get(compCtx, nil); err != nil {
		logger.Error("Failed to refund payment", "paymentID", paymentID, "error", err)
	} else {
		logger.Info("Payment refunded", "paymentID", paymentID)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers — no workflow I/O
// ---------------------------------------------------------------------------

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
// Failures are logged but never block the workflow.
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
