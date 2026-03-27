package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"
)

// FraudCheckActivity performs fraud screening before payment is charged.
// Introduced in OrderWorkflow v2.
type FraudCheckActivity struct {
	failureRate float64
}

func NewFraudCheckActivity(failureRate float64) *FraudCheckActivity {
	return &FraudCheckActivity{failureRate: failureRate}
}

type FraudCheckInput struct {
	OrderID    string
	CustomerID string
	Amount     float64
	Currency   string
	// IPAddress and DeviceID would come from the real request context in production.
	IPAddress string
	DeviceID  string
}

type FraudCheckResult struct {
	// Approved is true when the order passed all fraud checks.
	Approved bool
	// RiskScore is a 0–100 score; higher means riskier.
	RiskScore int
	// Reason is populated when Approved is false.
	Reason string
}

// FraudCheck screens an order for fraudulent signals.
// Idempotent — the activity ID is used as the check reference.
func (a *FraudCheckActivity) FraudCheck(ctx context.Context, input FraudCheckInput) (*FraudCheckResult, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	logger.Info("FraudCheck started",
		"orderID", input.OrderID,
		"customerID", input.CustomerID,
		"amount", input.Amount,
		"attempt", info.Attempt,
	)

	// Simulate network latency to external fraud service
	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	// Simulate transient service failure
	if rand.Float64() < a.failureRate {
		return nil, fmt.Errorf("fraud service unavailable: connection timeout")
	}

	// Simulate risk scoring
	riskScore := rand.Intn(100)

	logger.Info("FraudCheck risk score computed",
		"orderID", input.OrderID,
		"riskScore", riskScore,
	)

	// Simulate: high-value orders get extra scrutiny
	if input.Amount > 500 {
		riskScore = min(riskScore+15, 100)
		logger.Info("FraudCheck: high-value order risk adjustment",
			"orderID", input.OrderID,
			"adjustedScore", riskScore,
		)
	}

	// Score >= 80 → reject
	if riskScore >= 80 {
		logger.Warn("FraudCheck: order rejected",
			"orderID", input.OrderID,
			"riskScore", riskScore,
		)
		return &FraudCheckResult{
			Approved:  false,
			RiskScore: riskScore,
			Reason:    fmt.Sprintf("fraud risk score too high (%d/100)", riskScore),
		}, nil
	}

	logger.Info("FraudCheck passed",
		"orderID", input.OrderID,
		"riskScore", riskScore,
	)
	return &FraudCheckResult{
		Approved:  true,
		RiskScore: riskScore,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
