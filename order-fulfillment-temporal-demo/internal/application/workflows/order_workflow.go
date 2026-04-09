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
)

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

var stepOrder = map[OrderStep]int{
	StepInit:             0,
	StepReserveInventory: 1,
	StepFraudCheck:       2,
	StepChargePayment:    3,
	StepCreateShipment:   4,
	StepCompleted:        5,
}

type OrderWorkflowState struct {
	OrderID         string
	CurrentStep     OrderStep
	Status          string
	Priority        updates.OrderPriority
	ReservationID   string
	PaymentID       string
	ShipmentID      string
	ShippingAddress *ShippingAddress
	CancelRequested bool
	CancelReason    string
	LastUpdated     time.Time
}

func (s *OrderWorkflowState) IsTerminal() bool {
	return s.CurrentStep == StepCompleted ||
		s.CurrentStep == StepFailed ||
		s.CurrentStep == StepCancelled
}

func (s *OrderWorkflowState) HasCompleted(step OrderStep) bool {
	return stepOrder[s.CurrentStep] >= stepOrder[step]
}

func (s *OrderWorkflowState) CanTransitionTo(next OrderStep) bool {

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

func OrderWorkflow(ctx workflow.Context, input OrderWorkflowInput) (*OrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OrderWorkflow started", "orderID", input.OrderID, "customerID", input.CustomerID)

	state := &OrderWorkflowState{
		OrderID:     input.OrderID,
		CurrentStep: StepInit,
		Status:      string(StepInit),
		Priority:    updates.PriorityNormal,
		LastUpdated: workflow.Now(ctx),
	}

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

	cancelChannel := workflow.GetSignalChannel(ctx, signals.CancelOrderSignal)
	updateAddressChannel := workflow.GetSignalChannel(ctx, signals.UpdateShippingAddressSignal)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})

	fraudCheckVersion := workflow.GetVersion(
		ctx,
		OrderWorkflowChangeIDFraudCheck,
		workflow.DefaultVersion,
		OrderWorkflowV2FraudCheck,
	)

	selector := workflow.NewSelector(ctx)

	var (
		activityRunning bool
		activityCancel  workflow.CancelFunc
		childRunning    bool
		childCancel     workflow.CancelFunc
	)

	// paymentUpdateChan is registered once ChargePayment initiates.
	// It is nil until then so the selector ignores it.
	paymentUpdateChan := workflow.GetSignalChannel(ctx, signals.PaymentUpdateSignal)
	var (
		awaitingPaymentConfirm bool   // true while waiting for payment_update signal
		initiatedPaymentID     string // PaymentID returned by ChargePayment activity
	)

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

	// payment_update signal — only acted on while awaitingPaymentConfirm is true.
	// Signals arriving at other times are drained and discarded.
	selector.AddReceive(paymentUpdateChan, func(c workflow.ReceiveChannel, _ bool) {
		var sig signals.PaymentUpdatePayload
		c.Receive(ctx, &sig)

		if !awaitingPaymentConfirm {
			// Signal arrived outside the payment-wait window — discard.
			logger.Warn("payment_update signal received outside payment-wait window, discarding",
				"paymentID", sig.PaymentID, "status", sig.Status)
			return
		}

		if sig.PaymentID != initiatedPaymentID {
			// Belongs to a different payment — ignore.
			logger.Warn("payment_update signal for unknown paymentID, ignoring",
				"expected", initiatedPaymentID, "got", sig.PaymentID)
			return
		}

		logger.Info("payment_update signal received",
			"orderID", input.OrderID, "paymentID", sig.PaymentID, "status", sig.Status)

		switch sig.Status {
		case "SUCCESSFUL":
			awaitingPaymentConfirm = false
			state.PaymentID = sig.PaymentID
			logger.Info("Payment confirmed", "orderID", input.OrderID, "paymentID", sig.PaymentID)
			setStep(state, StepCreateShipment, ctx)

		case "FAILED", "CANCELED":
			awaitingPaymentConfirm = false
			logger.Warn("Payment failed/canceled",
				"orderID", input.OrderID, "paymentID", sig.PaymentID, "status", sig.Status)
			setStep(state, StepFailed, ctx)

		default:
			logger.Warn("payment_update: unknown status, ignoring",
				"status", sig.Status, "paymentID", sig.PaymentID)
		}
	})

	setStep(state, StepReserveInventory, ctx)

	for !state.IsTerminal() {

		// -- Launch work for the current step (only when nothing is running
		//    and we are not waiting for a payment confirmation signal) --------
		if !activityRunning && !childRunning && !awaitingPaymentConfirm {
			switch state.CurrentStep {

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

					// Activity succeeded — payment is "initiated".
					// DO NOT advance step yet. Wait for payment_update signal
					// from the payment microservice before proceeding.
					initiatedPaymentID = result.PaymentID
					awaitingPaymentConfirm = true
					logger.Info("ChargePayment initiated, waiting for payment_update signal",
						"orderID", input.OrderID, "paymentID", result.PaymentID)
				})

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

		selector.Select(ctx)

		if state.CancelRequested && !activityRunning && !childRunning && !state.IsTerminal() {
			logger.Warn("Handling cancellation", "orderID", input.OrderID, "reason", state.CancelReason)
			runCompensation(ctx, logger, state)
			setStep(state, StepCancelled, ctx)
			continue
		}
	}

	if state.CurrentStep == StepFailed {
		runCompensation(ctx, logger, state)
	}

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

func runCompensation(ctx workflow.Context, logger log.Logger, state *OrderWorkflowState) {
	if state.PaymentID != "" {
		compensatePayment(ctx, logger, state.PaymentID)
	}
	if state.ReservationID != "" {
		compensateInventory(ctx, logger, state.OrderID)
	}
}

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
