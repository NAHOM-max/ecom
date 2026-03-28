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

type PaymentActivity struct {
	failureRate float64
	producer    messaging.EventProducer
	idempotency idempotency.Store
}

func NewPaymentActivity(failureRate float64, producer messaging.EventProducer, store idempotency.Store) *PaymentActivity {
	return &PaymentActivity{failureRate: failureRate, producer: producer, idempotency: store}
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

	paymentID := fmt.Sprintf("pay-%s", info.ActivityID)
	idemKey := "ChargePayment:" + paymentID

	// If this payment was already charged on a previous attempt, return the
	// cached result immediately — the customer must never be charged twice.
	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached ChargePaymentResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("ChargePayment: returning cached result (idempotent)",
				"orderID", input.OrderID, "paymentID", paymentID)
			return &cached, nil
		}
	}

	transactionID := fmt.Sprintf("txn-%s-%d", info.ActivityID, time.Now().Unix())

	logger.Info("ChargePayment started",
		"orderID", input.OrderID,
		"customerID", input.CustomerID,
		"amount", input.Amount,
		"paymentID", paymentID,
		"attempt", info.Attempt)

	time.Sleep(time.Duration(200+rand.Intn(800)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		// Transient failure — do NOT cache, allow retry.
		return nil, fmt.Errorf("payment gateway unavailable: network timeout")
	}

	// Business decline — cache so retries return the same decline.
	if rand.Float64() < 0.03 {
		result := &ChargePaymentResult{
			PaymentID:     paymentID,
			Status:        "declined",
			TransactionID: transactionID,
			Message:       "Payment declined: insufficient funds",
		}
		_ = a.idempotency.Save(ctx, idemKey, result)
		return result, nil
	}

	// Fraud detection — cache so retries return the same rejection.
	if rand.Float64() < 0.01 {
		result := &ChargePaymentResult{
			PaymentID:     paymentID,
			Status:        "fraud_detected",
			TransactionID: transactionID,
			Message:       "Payment flagged for potential fraud",
		}
		_ = a.idempotency.Save(ctx, idemKey, result)
		return result, nil
	}

	result := &ChargePaymentResult{
		PaymentID:     paymentID,
		Status:        "charged",
		TransactionID: transactionID,
		Message:       "Payment processed successfully",
	}

	// Save before publishing — guarantees the charge is recorded even if
	// the process crashes between Save and Publish.
	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("ChargePayment: failed to save idempotency record", "error", err)
	}

	_ = a.producer.Publish(messaging.TopicPayments, messaging.Event{
		EventID:   fmt.Sprintf("evt-%s", paymentID),
		EventType: messaging.EventPaymentCharged,
		Timestamp: time.Now(),
		OrderID:   input.OrderID,
		Payload: messaging.PaymentChargedPayload{
			PaymentID:     paymentID,
			TransactionID: transactionID,
			Amount:        input.Amount,
			Currency:      input.Currency,
		},
	})

	logger.Info("ChargePayment completed",
		"orderID", input.OrderID, "paymentID", paymentID, "amount", input.Amount)
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

	refundID := fmt.Sprintf("ref-%s-%d", paymentID, time.Now().Unix())
	logger.Info("RefundPayment started", "paymentID", paymentID, "refundID", refundID, "attempt", info.Attempt)

	time.Sleep(time.Duration(200+rand.Intn(600)) * time.Millisecond)

	if rand.Float64() < a.failureRate*0.5 {
		return fmt.Errorf("payment gateway unavailable: network timeout")
	}

	if err := a.idempotency.Save(ctx, idemKey, map[string]string{"refund_id": refundID, "status": "refunded"}); err != nil {
		logger.Warn("RefundPayment: failed to save idempotency record", "error", err)
	}

	logger.Info("RefundPayment completed", "paymentID", paymentID, "refundID", refundID)
	return nil
}

func (a *PaymentActivity) VerifyPayment(ctx context.Context, paymentID string) (string, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	logger.Info("VerifyPayment started", "paymentID", paymentID, "attempt", info.Attempt)
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		return "", fmt.Errorf("payment gateway unavailable: connection timeout")
	}

	logger.Info("VerifyPayment completed", "paymentID", paymentID, "status", "charged")
	return "charged", nil
}
