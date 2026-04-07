package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/domain"
)

const inventoryServiceURL = "http://localhost:5000/reserve"
const inventoryReleaseURL = "http://localhost:5000/return"

type InventoryClient struct {
	client *http.Client
}

func NewInventoryClient() *InventoryClient {
	return &InventoryClient{
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

type reserveItem struct {
	ProductID string `json:"product_id"`
	Amount    int    `json:"amount"`
}

type reserveRequest struct {
	OrderID string        `json:"order_id"`
	Items   []reserveItem `json:"items"`
}

type reserveSuccessResponse struct {
	ReservationID string `json:"reservation_id"`
}

type reserveErrorResponse struct {
	Error string `json:"error"`
}

type releaseRequest struct {
	ReservationID string `json:"order_id"`
}

func (c *InventoryClient) Release(ctx context.Context, reservationID string) error {
	body, err := json.Marshal(releaseRequest{ReservationID: reservationID})
	if err != nil {
		return fmt.Errorf("inventory client: marshal release request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inventoryReleaseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("inventory client: build release request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		// Network / timeout — retryable
		return fmt.Errorf("inventory client: release request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp reserveErrorResponse
		if jsonErr := json.NewDecoder(resp.Body).Decode(&errResp); jsonErr != nil || errResp.Error == "" {
			return fmt.Errorf("inventory client: release failed: status %d", resp.StatusCode)
		}
		// Non-200 — retryable (compensation must succeed)
		return fmt.Errorf("inventory client: release failed: %s", errResp.Error)
	}

	return nil
}

func (c *InventoryClient) Reserve(ctx context.Context, orderID string, items []domain.ReserveItem) (*domain.ReserveResponse, error) {
	reqItems := make([]reserveItem, len(items))
	for i, it := range items {
		reqItems[i] = reserveItem{ProductID: it.ProductID, Amount: it.Quantity}
	}

	body, err := json.Marshal(reserveRequest{OrderID: orderID, Items: reqItems})
	if err != nil {
		return nil, fmt.Errorf("inventory client: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inventoryServiceURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("inventory client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		// Network error — retryable by Temporal
		return nil, fmt.Errorf("inventory client: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp reserveErrorResponse
		if jsonErr := json.NewDecoder(resp.Body).Decode(&errResp); jsonErr != nil || errResp.Error == "" {
			return nil, fmt.Errorf("inventory service error: status %d", resp.StatusCode)
		}
		// Business failure — non-retryable
		return nil, fmt.Errorf("inventory service error: %s", errResp.Error)
	}

	var success reserveSuccessResponse
	if err := json.NewDecoder(resp.Body).Decode(&success); err != nil {
		return nil, fmt.Errorf("inventory client: decode response: %w", err)
	}
	if success.ReservationID == "" {
		return nil, fmt.Errorf("inventory client: missing reservation_id in response")
	}

	return &domain.ReserveResponse{ReservationID: success.ReservationID}, nil
}
