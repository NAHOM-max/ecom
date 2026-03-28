package activities

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/idempotency"
)

type FraudCheckActivity struct {
	failureRate float64
	idempotency idempotency.Store
}

func NewFraudCheckActivity(failureRate float64, store idempotency.Store) *FraudCheckActivity {
	return &FraudCheckActivity{failureRate: failureRate, idempotency: store}
}

type FraudCheckInput struct {
	OrderID    string
	CustomerID string
	Amount     float64
	Currency   string
	IPAddress  string
	DeviceID   string
}

type FraudCheckResult struct {
	Approved  bool
	RiskScore int
	Reason    string
}

func (a *FraudCheckActivity) FraudCheck(ctx context.Context, input FraudCheckInput) (*FraudCheckResult, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)

	// Key on order ID — a fraud decision for an order must be consistent
	// across retries. We never want to approve on retry what was rejected first.
	idemKey := "FraudCheck:" + input.OrderID

	if exists, _ := a.idempotency.Exists(ctx, idemKey); exists {
		var cached FraudCheckResult
		if err := a.idempotency.Load(ctx, idemKey, &cached); err == nil {
			logger.Info("FraudCheck: returning cached result (idempotent)",
				"orderID", input.OrderID, "approved", cached.Approved)
			return &cached, nil
		}
	}

	logger.Info("FraudCheck started",
		"orderID", input.OrderID,
		"customerID", input.CustomerID,
		"amount", input.Amount,
		"attempt", info.Attempt)

	time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

	if rand.Float64() < a.failureRate {
		// Transient failure — do NOT cache, allow retry.
		return nil, fmt.Errorf("fraud service unavailable: connection timeout")
	}

	riskScore := rand.Intn(100)
	if input.Amount > 500 {
		riskScore = min(riskScore+15, 100)
	}

	var result *FraudCheckResult
	if riskScore >= 80 {
		result = &FraudCheckResult{
			Approved:  false,
			RiskScore: riskScore,
			Reason:    fmt.Sprintf("fraud risk score too high (%d/100)", riskScore),
		}
	} else {
		result = &FraudCheckResult{Approved: true, RiskScore: riskScore}
	}

	// Cache the decision — fraud verdicts must be deterministic across retries.
	if err := a.idempotency.Save(ctx, idemKey, result); err != nil {
		logger.Warn("FraudCheck: failed to save idempotency record", "error", err)
	}

	logger.Info("FraudCheck completed",
		"orderID", input.OrderID, "approved", result.Approved, "riskScore", riskScore)
	return result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
