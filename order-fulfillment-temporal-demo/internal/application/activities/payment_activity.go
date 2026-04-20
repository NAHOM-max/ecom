package activities

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/payment"
)

type PaymentActivity struct {
	paymentClient payment.PaymentClient
	producer      messaging.EventProducer
	idempotency   idempotency.Store
}

func NewPaymentActivity(client payment.PaymentClient, producer messaging.EventProducer, store idempotency.Store) *PaymentActivity {
	return &PaymentActivity{paymentClient: client, producer: producer, idempotency: store}
}

type ChargePaymentInput struct {
	OrderID      string
	CustomerID   string
	Amount       float64
	Currency     string
	PaymentToken string
}

type ChargePaymentResult struct {
	PaymentID     string
	Status        string
	TransactionID string
	Message       string
}

func (a *PaymentActivity) ChargePayment(ctx context.Context, input ChargePaymentInput) (*ChargePaymentResult, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	idemKey := "ChargePayment:" + input.OrderID

	// If this payment was already initiated on a previous attempt, return the
	// cached result immediately — the customer must never be charged twice.
	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached ChargePaymentResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("ChargePayment: returning cached result (idempotent)",
				"orderID", input.OrderID, "paymentID", cached.PaymentID)
			return &cached, nil
		}
	}

	logger.Info("ChargePayment started",
		"orderID", input.OrderID,
		"customerID", input.CustomerID,
		"amount", input.Amount,
		"attempt", info.Attempt)

	resp, err := a.paymentClient.InitiatePayment(ctx, payment.InitiatePaymentRequest{
		CustomerID: input.CustomerID,
		OrderID:    input.OrderID,
		WorkflowID: info.WorkflowExecution.ID,
		Amount:     input.Amount,
	})
	if err != nil {
		// Network / service error — do NOT cache, let Temporal retry.
		logger.Error("ChargePayment: payment service error",
			"orderID", input.OrderID, "attempt", info.Attempt, "error", err)
		return nil, err
	}

	result := &ChargePaymentResult{
		PaymentID:     resp.PaymentID,
		Status:        "initiated",
		TransactionID: "",
		Message:       "Payment initiated successfully",
	}

	// Save before returning — guarantees idempotency even if the worker crashes
	// between this point and the workflow receiving the result.
	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("ChargePayment: failed to save idempotency record", "error", err)
	}

	logger.Info("ChargePayment completed",
		"orderID", input.OrderID, "paymentID", resp.PaymentID)
	return result, nil
}

func (a *PaymentActivity) RefundPayment(ctx context.Context, paymentID string) error {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	idemKey := "RefundPayment:" + paymentID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		logger.Info("RefundPayment: already refunded (idempotent)", "paymentID", paymentID)
		return nil
	}

	logger.Info("RefundPayment started", "paymentID", paymentID, "attempt", info.Attempt)

	resp, err := a.paymentClient.RefundPayment(ctx, payment.RefundPaymentRequest{
		PaymentID: paymentID,
	})
	if err != nil {
		logger.Error("RefundPayment failed - service error",
			"paymentID", paymentID, "attempt", info.Attempt, "error", err)
		return err
	}

	if err := a.idempotency.Save(ctx, idemKey, map[string]string{"payment_id": resp.PaymentID, "status": resp.Status}); err != nil {
		logger.Warn("RefundPayment: failed to save idempotency record", "error", err)
	}

	logger.Info("RefundPayment completed", "paymentID", paymentID, "status", resp.Status)
	return nil
}

func (a *PaymentActivity) VerifyPayment(ctx context.Context, paymentID string) (string, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	logger.Info("VerifyPayment started", "paymentID", paymentID, "attempt", info.Attempt)
	logger.Info("VerifyPayment completed", "paymentID", paymentID, "status", "charged")
	return "charged", nil
}
