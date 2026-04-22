package shipment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ShipmentClient is the abstraction the activity depends on.
type ShipmentClient interface {
	CreateShipment(ctx context.Context, req CreateShipmentRequest) (*ShipmentResponse, error)
}

// ---------------------------------------------------------------------------
// Request / Response models
// ---------------------------------------------------------------------------

type CreateShipmentRequest struct {
	OrderID        string         `json:"order_id"`
	OrderCreatedAt time.Time      `json:"order_created_at"`
	Address        AddressRequest `json:"address"`
	WorkflowID     string         `json:"workflow_id"`
}

type AddressRequest struct {
	Name    string `json:"name"`
	Street  string `json:"street"`
	City    string `json:"city"`
	Country string `json:"country"`
}

type AddressResponse struct {
	Name    string `json:"name"`
	Street  string `json:"street"`
	City    string `json:"city"`
	Country string `json:"country"`
}

type ShipmentResponse struct {
	ID             string          `json:"id"`
	OrderID        string          `json:"order_id"`
	TrackingNumber string          `json:"tracking_number"`
	DeliveryDate   time.Time       `json:"delivery_date"`
	Status         string          `json:"status"`
	Confirmed      bool            `json:"confirmed"`
	Address        AddressResponse `json:"address"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// HTTPShipmentClient
// ---------------------------------------------------------------------------

type HTTPShipmentClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPShipmentClient(baseURL string) *HTTPShipmentClient {
	return &HTTPShipmentClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func (c *HTTPShipmentClient) CreateShipment(ctx context.Context, req CreateShipmentRequest) (*ShipmentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("shipment client: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/shipments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("shipment client: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		// Network error — retryable by Temporal
		return nil, fmt.Errorf("shipment client: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return nil, fmt.Errorf("shipment service error: %s", errResp.Error)
		}
		return nil, fmt.Errorf("shipment service error: status %d", resp.StatusCode)
	}

	var result ShipmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("shipment client: decode response: %w", err)
	}

	return &result, nil
}
