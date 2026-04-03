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

// ---------------------------------------------------------------------------
// Input / Output types
// ---------------------------------------------------------------------------

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

type OrderWorkflowResult struct {
	OrderID    string
	Status     string
	PaymentID  string
	ShipmentID string
	Message    string
}

// ---------------------------------------------------------------------------
// Step enum
// ---------------------------------------------------------------------------

// OrderStep is the typed enum for workflow step transitions.
// CurrentStep is the single source of truth — Status is always derived from it.
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

// stepOrder defines the forward progression index of each step.
// Terminal steps (Failed, Cancelled) are intentionally absent — they are
// not part of the forward chain and must never be used in HasCompleted.
var stepOrder = map[OrderStep]int{
	StepInit:             0,
	StepReserveInventory: 1,
	StepFraudCheck:       2,
	StepChargePayment:    3,
	StepCreateShipment:   4,
	StepCompleted:        5,
}

// ---------------------------------------------------------------------------
// State — cleaned, no redundant boolean flags
// ---------------------------------------------------------------------------

// OrderWorkflowState is the complete, serialisable workflow state.
// Boolean flags (InventoryReserved, PaymentCharged, ShipmentCreated) have been
// removed — step progression and IDs are the sole source of truth.
type OrderWorkflowState struct {
	OrderID         string
	CurrentStep     OrderStep
	Status          string // always string(CurrentStep)
	Priority        updates.OrderPriority
	ReservationID   string
	PaymentID       string
	ShipmentID      string
	ShippingAddress *ShippingAddress
	CancelRequested bool
	CancelReason    string
	LastUpdated     time.Time
}

// IsTerminal returns true when the workflow has reached a final step.
func (s *OrderWorkflowState) IsTerminal() bool {
	return s.CurrentStep == StepCompleted ||
		s.CurrentStep == StepFailed ||
		s.CurrentStep == StepCancelled
}

// HasCompleted returns true when the workflow has reached or passed step in
// the forward chain. Used to decide which compensations are needed.
// Must not be called with terminal steps (StepFailed, StepCancelled).
func (s *OrderWorkflowState) HasCompleted(step OrderStep) bool {
	return stepOrder[s.CurrentStep] >= stepOrder[step]
}

// CanTransitionTo returns true when next is a valid forward or terminal
// transition from the current step.
func (s *OrderWorkflowState) CanTransitionTo(next OrderStep) bool {
	// Any non-terminal step may transition to a terminal step.
	if next == StepFailed || next == StepCancelled {
		return !s.IsTerminal()
	}
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

// ---------------------------------------------------------------------------
// setStep — enforced transitions
// ---------------------------------------------------------------------------

// setStep is the only place that mutates CurrentStep.
// It panics on invalid transitions so bugs surface immediately during testing
// rather than silently corrupting state in production.
func setStep(state *OrderWorkflowState, next OrderStep, ctx workflow.Context) {
	if !state.CanTransitionTo(next) {
		panic(fmt.Sprintf(
			"invalid workflow transition: %s → %s (orderID=%s)",
			state.CurrentStep, next, state.OrderID,
		))
	}
	state.CurrentStep = next
	state.Status = string(next)
	state.LastUpdated = workflow.Now(ctx)
}

// ---------------------------------------------------------------------------
// OrderWorkflow
// ---------------------------------------------------------------------------

// OrderWorkflow orchestrates the complete order fulfillment process.
//
// Architecture: single event-driven selector loop driven by OrderStep.
//
//	INIT → RESERVE_INVENTORY → [FRAUD_CHECK] → CHARGE_PAYMENT → CREATE_SHIPMENT → COMPLETED
//	                                                          ↘ FAILED / CANCELLED (any step)
func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OrderWorkflow started", "orderID", input.OrderID, "customerID", input.CustomerID)

	// -------------------------------------------------------------------------
	// State initialisation
	// -------------------------------------------------------------------------
	state := &OrderWorkflowState{
		OrderID:     input.OrderID,
		CurrentStep: StepInit,
		Status:      string(StepInit),
		Priority:    updates.PriorityNormal,
		LastUpdated: workflow.Now(ctx),
	}

	// -------------------------------------------------------------------------
	// Query handler
	// -------------------------------------------------------------------------
	if err := workflow.SetQueryHandler(ctx, queries.OrderStatusQuery,
		func() (queries.OrderStatusResult, error) {
			return queries.OrderStatusResult{
				OrderID:        state.OrderID,
				CurrentStatus:  state.Status,
				PaymentStatus:  paymentStatus(state),
				ShipmentStatus: shipmentStatus(state),
				Priority:       string(state.Priority),
			}, nil
		},
	); err != nil {
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
	// Activity options
	// -------------------------------------------------------------------------
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})

	// -------------------------------------------------------------------------
	// Versioning — called ONCE before the loop so the history marker is
	// recorded at a fixed deterministic point.
	// -------------------------------------------------------------------------
	fraudCheckVersion := workflow.GetVersion(
		ctx,
		OrderWorkflowChangeIDFraudCheck,
		workflow.DefaultVersion,
		OrderWorkflowV2FraudCheck,
	)

	// -------------------------------------------------------------------------
	// Loop control
	// -------------------------------------------------------------------------
	selector := workflow.NewSelector(ctx)

	var (
		activityRunning bool
		activityCancel  workflow.CancelFunc
		childRunning    bool
		childCancel     workflow.CancelFunc
	)

	// -------------------------------------------------------------------------
	// Global signal handlers — registered once, re-armed by Temporal on each
	// new delivery.
	// -------------------------------------------------------------------------

	// Cancel signal: record intent and stop in-flight work.
	// Compensation runs in the loop body — not here.
	selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, _ bool) {
		var req signals.CancelOrderRequest
		c.Receive(ctx, &req)

		if state.CancelRequested {
			return
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

	// Address update signal: drain and apply; ignore after cancellation.
	selector.AddReceive(updateAddressChannel, func(c workflow.ReceiveChannel, _ bool) {
		var req signals.UpdateShippingAddressRequest
		if state.CancelRequested {
			for c.ReceiveAsync(&req) {
			}
			return
		}
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
	// Advance past INIT before entering the loop
	// -------------------------------------------------------------------------
	setStep(state, StepReserveInventory, ctx)

	// -------------------------------------------------------------------------
	// Main event-driven loop
	// -------------------------------------------------------------------------
	for !state.IsTerminal() {

		// -- Launch work for the current step (only when nothing is running) --
		if !activityRunning && !childRunning {
			switch state.CurrentStep {

			// -----------------------------------------------------------------
			case StepReserveInventory:
				logger.Info("Starting ReserveInventory", "orderID", input.OrderID)
				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				f := workflow.ExecuteActivity(actCtx, "ReserveInventory",
					activities.ReserveInventoryInput{
						OrderID: input.OrderID,
						Items:   convertToInventoryItems(input.Items),
					})

				selector.AddFuture(f, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					var result activities.ReserveInventoryResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ReserveInventory failed", "error", err)
						setStep(state, StepFailed, ctx)
						return
					}
					if !result.Success {
						logger.Error("ReserveInventory business error", "message", result.Message)
						setStep(state, StepFailed, ctx)
						return
					}

					state.ReservationID = result.ReservationID
					logger.Info("ReserveInventory succeeded", "reservationID", result.ReservationID)

					if fraudCheckVersion >= OrderWorkflowV2FraudCheck {
						setStep(state, StepFraudCheck, ctx)
					} else {
						setStep(state, StepChargePayment, ctx)
					}
				})

			// -----------------------------------------------------------------
			case StepFraudCheck:
				logger.Info("Starting FraudCheck", "orderID", input.OrderID)
				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				f := workflow.ExecuteActivity(actCtx, "FraudCheck",
					activities.FraudCheckInput{
						OrderID:    input.OrderID,
						CustomerID: input.CustomerID,
						Amount:     calculateTotal(input.Items),
						Currency:   "USD",
					})

				selector.AddFuture(f, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					var result activities.FraudCheckResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("FraudCheck failed", "error", err)
						setStep(state, StepFailed, ctx)
						return
					}
					if !result.Approved {
						logger.Warn("FraudCheck rejected",
							"riskScore", result.RiskScore, "reason", result.Reason)
						setStep(state, StepFailed, ctx)
						return
					}

					logger.Info("FraudCheck passed", "riskScore", result.RiskScore)
					setStep(state, StepChargePayment, ctx)
				})

			// -----------------------------------------------------------------
			case StepChargePayment:
				logger.Info("Starting ChargePayment", "orderID", input.OrderID)
				actCtx, cancel := workflow.WithCancel(ctx)
				activityCancel = cancel
				activityRunning = true

				f := workflow.ExecuteActivity(actCtx, "ChargePayment",
					activities.ChargePaymentInput{
						OrderID:    input.OrderID,
						CustomerID: input.CustomerID,
						Amount:     calculateTotal(input.Items),
						Currency:   "USD",
					})

				selector.AddFuture(f, func(f workflow.Future) {
					activityRunning = false
					activityCancel = nil

					var result activities.ChargePaymentResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ChargePayment failed", "error", err)
						setStep(state, StepFailed, ctx)
						return
					}
					if result.Status != "charged" {
						logger.Error("ChargePayment declined",
							"status", result.Status, "message", result.Message)
						setStep(state, StepFailed, ctx)
						return
					}

					state.PaymentID = result.PaymentID
					logger.Info("ChargePayment succeeded", "paymentID", result.PaymentID)
					setStep(state, StepCreateShipment, ctx)
				})

			// -----------------------------------------------------------------
			case StepCreateShipment:
				logger.Info("Starting ShipmentWorkflow", "orderID", input.OrderID)

				shippingAddress := ShippingAddress{
					Name: "Customer Name", Street: "123 Main St",
					City: "New York", State: "NY",
					PostalCode: "10001", Country: "USA", Phone: "555-0100",
				}
				if state.ShippingAddress != nil {
					shippingAddress = *state.ShippingAddress
					logger.Info("Using updated shipping address", "city", shippingAddress.City)
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

				cf := workflow.ExecuteChildWorkflow(childCtx, ShipmentWorkflow,
					ShipmentWorkflowInput{
						OrderID:         input.OrderID,
						CustomerAddress: shippingAddress,
						Items:           []ShipmentItem{{ProductID: "prod-1", Quantity: 1, Weight: 2.5, Description: "Product"}},
						ShippingMethod:  "standard",
					})

				selector.AddFuture(cf, func(f workflow.Future) {
					childRunning = false
					childCancel = nil

					var result ShipmentWorkflowResult
					if err := f.Get(ctx, &result); err != nil {
						logger.Error("ShipmentWorkflow failed", "error", err)
						setStep(state, StepFailed, ctx)
						return
					}
					if !result.Success {
						logger.Error("ShipmentWorkflow business error", "message", result.Message)
						setStep(state, StepFailed, ctx)
						return
					}

					state.ShipmentID = result.ShipmentID
					logger.Info("ShipmentWorkflow succeeded", "shipmentID", result.ShipmentID)
					setStep(state, StepCompleted, ctx)
				})
			}
		}

		// -- Wait for the next event -------------------------------------------
		selector.Select(ctx)

		// -- Centralised cancellation: runs once nothing is in-flight ----------
		if state.CancelRequested && !activityRunning && !childRunning && !state.IsTerminal() {
			logger.Warn("Handling cancellation", "orderID", input.OrderID, "reason", state.CancelReason)
			runCompensation(ctx, logger, state)
			setStep(state, StepCancelled, ctx)
			continue
		}

		// -- Centralised failure compensation ---------------------------------
		// When a callback sets StepFailed, compensation runs here so the
		// callback itself stays free of compensation logic.
		if state.CurrentStep == StepFailed && !state.IsTerminal() {
			// IsTerminal() is true for StepFailed, so this block is unreachable
			// after the first iteration — kept as a safety net.
		}
	}

	// -------------------------------------------------------------------------
	// Post-loop: run compensation for failure path
	// (callbacks set StepFailed; compensation runs here, not inside callbacks)
	// -------------------------------------------------------------------------
	if state.CurrentStep == StepFailed {
		runCompensation(ctx, logger, state)
	}

	// -------------------------------------------------------------------------
	// Build result from terminal state
	// -------------------------------------------------------------------------
	logger.Info("OrderWorkflow finished",
		"orderID", input.OrderID,
		"step", state.CurrentStep,
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
		return &OrderWorkflowResult{
			OrderID: input.OrderID,
			Status:  string(StepFailed),
			Message: "Order failed",
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Compensation helpers
// ---------------------------------------------------------------------------

// compensateInventory releases reserved inventory using a disconnected context
// so it runs even after workflow cancellation.
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

// compensatePayment refunds the payment using a disconnected context.
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

// runCompensation undoes completed steps in reverse order (payment before inventory).
// It uses the presence of IDs — not HasCompleted — as the source of truth,
// because HasCompleted is unreliable when CurrentStep is a terminal value
// (StepFailed / StepCancelled) which is absent from stepOrder.
func runCompensation(ctx workflow.Context, logger log.Logger, state *OrderWorkflowState) {
	// Reverse order: payment before inventory (last-in, first-out).
	if state.PaymentID != "" {
		compensatePayment(ctx, logger, state.PaymentID)
	}
	if state.ReservationID != "" {
		compensateInventory(ctx, logger, state.ReservationID)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func convertToInventoryItems(items []OrderItemInput) []activities.InventoryItem {
	result := make([]activities.InventoryItem, len(items))
	for i, item := range items {
		result[i] = activities.InventoryItem{ProductID: item.ProductID, Quantity: item.Quantity}
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

// paymentStatus derives a readable payment status.
// Uses PaymentID presence rather than HasCompleted so it is correct in
// terminal states where stepOrder has no entry for StepFailed/StepCancelled.
func paymentStatus(state *OrderWorkflowState) string {
	switch {
	case state.CancelRequested && state.PaymentID != "":
		return "refunded"
	case state.PaymentID != "":
		return "charged"
	case state.CurrentStep == StepChargePayment:
		return "pending"
	default:
		return "not_started"
	}
}

// shipmentStatus derives a readable shipment status.
// Uses ShipmentID presence rather than HasCompleted for the same reason.
func shipmentStatus(state *OrderWorkflowState) string {
	switch {
	case state.CancelRequested && state.ShipmentID != "":
		return "cancelled"
	case state.ShipmentID != "":
		return "created"
	case state.CurrentStep == StepCreateShipment:
		return "pending"
	default:
		return "not_started"
	}
}

// publishEvent fires a PublishEvent activity with best-effort retry.
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
			"eventType", event.EventType, "orderID", event.OrderID, "error", err)
	}
}
