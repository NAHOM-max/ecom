package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
)

// PaymentActivity simulates payment service operations
type PaymentActivity struct {
	failureRate float64
	producer    messaging.EventProducer
}

// NewPaymentActivity creates a new payment activity
func NewPaymentActivity(failureRate float64, producer messaging.EventProducer) *PaymentActivity {
	return &PaymentActivity{failureRate: failureRate, producer: producer}
}

// ChargePaymentInput contains payment processing parameters
type ChargePaymentInput struct {
	OrderID      string
	CustomerID   string
	Amount       float64
	Currency     string
	PaymentToken string
}

// ChargePaymentResult contains the payment result
type ChargePaymentResult struct {
	PaymentID     string
	Status        string
	TransactionID string
	Message       string
}

// ChargePayment processes a payment for an order
// Idempotent - uses activity ID as payment ID
func (a *PaymentActivity) ChargePayment(ctx context.Context, input ChargePaymentInput) (*ChargePaymentResult, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	// Generate idempotent payment ID based on activity ID
	paymentID := fmt.Sprintf("pay-%s", activityInfo.ActivityID)
	transactionID := fmt.Sprintf("txn-%s-%d", activityInfo.ActivityID, time.Now().Unix())

	logger.Info("ChargePayment started",
		"orderID", input.OrderID,
		"customerID", input.CustomerID,
		"amount", input.Amount,
		"currency", input.Currency,
		"paymentID", paymentID,
		"attempt", activityInfo.Attempt)

	// Simulate network delay (payment processing takes longer)
	time.Sleep(time.Duration(200+rand.Intn(800)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("ChargePayment failed - simulated service error",
			"orderID", input.OrderID,
			"paymentID", paymentID,
			"attempt", activityInfo.Attempt)
		return nil, fmt.Errorf("payment gateway unavailable: network timeout")
	}

	// Simulate payment validation
	logger.Info("Validating payment details",
		"customerID", input.CustomerID,
		"amount", input.Amount)

	// Simulate occasional payment declined (non-retryable business error)
	if rand.Float64() < 0.03 { // 3% chance
		logger.Error("Payment declined",
			"orderID", input.OrderID,
			"customerID", input.CustomerID,
			"reason", "insufficient_funds")
		return &ChargePaymentResult{
			PaymentID:     paymentID,
			Status:        "declined",
			TransactionID: transactionID,
			Message:       "Payment declined: insufficient funds",
		}, nil // Return success with declined status (business error)
	}

	// Simulate fraud check
	logger.Info("Running fraud detection", "customerID", input.CustomerID)
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// Simulate occasional fraud detection (non-retryable)
	if rand.Float64() < 0.01 { // 1% chance
		logger.Error("Payment flagged for fraud",
			"orderID", input.OrderID,
			"customerID", input.CustomerID)
		return &ChargePaymentResult{
			PaymentID:     paymentID,
			Status:        "fraud_detected",
			TransactionID: transactionID,
			Message:       "Payment flagged for potential fraud",
		}, nil
	}

	// Simulate processing payment
	logger.Info("Processing payment charge",
		"paymentID", paymentID,
		"amount", input.Amount)

	logger.Info("ChargePayment completed successfully",
		"orderID", input.OrderID,
		"paymentID", paymentID,
		"transactionID", transactionID,
		"amount", input.Amount)

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

	return &ChargePaymentResult{
		PaymentID:     paymentID,
		Status:        "charged",
		TransactionID: transactionID,
		Message:       "Payment processed successfully",
	}, nil
}

// RefundPayment refunds a payment (compensation)
// Idempotent - safe to call multiple times
func (a *PaymentActivity) RefundPayment(ctx context.Context, paymentID string) error {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	refundID := fmt.Sprintf("ref-%s-%d", paymentID, time.Now().Unix())

	logger.Info("RefundPayment started",
		"paymentID", paymentID,
		"refundID", refundID,
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(200+rand.Intn(600)) * time.Millisecond)

	// Simulate service instability (lower failure rate for refunds)
	if rand.Float64() < (a.failureRate * 0.5) { // Half the failure rate
		logger.Warn("RefundPayment failed - simulated service error",
			"paymentID", paymentID,
			"refundID", refundID,
			"attempt", activityInfo.Attempt)
		return fmt.Errorf("payment gateway unavailable: network timeout")
	}

	// Idempotent: Check if already refunded (simulate)
	logger.Info("Checking payment status", "paymentID", paymentID)

	// Simulate processing refund
	logger.Info("Processing refund", "paymentID", paymentID, "refundID", refundID)

	logger.Info("RefundPayment completed successfully",
		"paymentID", paymentID,
		"refundID", refundID)

	return nil
}

// VerifyPayment verifies payment status
func (a *PaymentActivity) VerifyPayment(ctx context.Context, paymentID string) (string, error) {
	logger := activity.GetLogger(ctx)
	activityInfo := activity.GetInfo(ctx)

	logger.Info("VerifyPayment started",
		"paymentID", paymentID,
		"attempt", activityInfo.Attempt)

	// Simulate network delay
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	// Simulate service instability
	if rand.Float64() < a.failureRate {
		logger.Warn("VerifyPayment failed - simulated service error",
			"paymentID", paymentID,
			"attempt", activityInfo.Attempt)
		return "", fmt.Errorf("payment gateway unavailable: connection timeout")
	}

	status := "charged"
	logger.Info("VerifyPayment completed successfully",
		"paymentID", paymentID,
		"status", status)

	return status, nil
}
