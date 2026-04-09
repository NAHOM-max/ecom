package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PaymentClient is the abstraction the activity depends on.
// Swap HTTPPaymentClient for a stub in tests without touching activity code.
type PaymentClient interface {
	InitiatePayment(ctx context.Context, req InitiatePaymentRequest) (*InitiatePaymentResponse, error)
}

// InitiatePaymentRequest is the body sent to the payment microservice.
type InitiatePaymentRequest struct {
	CustomerID string  `json:"customer_id"`
	OrderID    string  `json:"order_id"`
	WorkflowID string  `json:"workflow_id"`
	Amount     float64 `json:"amount"`
}

// InitiatePaymentResponse is returned on HTTP 201.
type InitiatePaymentResponse struct {
	PaymentID string `json:"payment_id"`
}

// ---------------------------------------------------------------------------

// HTTPPaymentClient calls the real payment microservice.
type HTTPPaymentClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPPaymentClient(baseURL string) *HTTPPaymentClient {
	return &HTTPPaymentClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func (c *HTTPPaymentClient) InitiatePayment(ctx context.Context, req InitiatePaymentRequest) (*InitiatePaymentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("payment client: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/payments/initiate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("payment client: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		// Network error — retryable by Temporal
		return nil, fmt.Errorf("payment client: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return nil, fmt.Errorf("payment service error: %s", errResp.Error)
		}
		return nil, fmt.Errorf("payment service error: status %d", resp.StatusCode)
	}

	var success InitiatePaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&success); err != nil {
		return nil, fmt.Errorf("payment client: decode response: %w", err)
	}
	if success.PaymentID == "" {
		return nil, fmt.Errorf("payment client: missing payment_id in response")
	}

	return &success, nil
}
