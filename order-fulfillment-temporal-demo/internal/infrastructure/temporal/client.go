package temporal

import (
	"context"
	"fmt"
	"log"

	"go.temporal.io/sdk/client"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/queries"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/signals"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/updates"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/workflows"
)

// Task queue constants
const (
	OrderFulfillmentTaskQueue = "order-fulfillment"
)

// Client wraps Temporal SDK client
type Client struct {
	client client.Client
	config *Config
}

// Config contains Temporal client configuration
type Config struct {
	HostPort  string
	Namespace string
}

// NewClient creates a new Temporal client
func NewClient(config *Config) (*Client, error) {
	if config.HostPort == "" {
		config.HostPort = "localhost:7233"
	}
	if config.Namespace == "" {
		config.Namespace = "default"
	}

	c, err := client.Dial(client.Options{
		HostPort:  config.HostPort,
		Namespace: config.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	log.Printf("Temporal client connected to %s (namespace: %s)", config.HostPort, config.Namespace)

	return &Client{
		client: c,
		config: config,
	}, nil
}

// StartOrderWorkflow starts a new OrderWorkflow execution.
func (c *Client) StartOrderWorkflow(ctx context.Context, input workflows.OrderWorkflowInput) (client.WorkflowRun, error) {
	options := client.StartWorkflowOptions{
		ID:        "order-" + input.OrderID,
		TaskQueue: OrderFulfillmentTaskQueue,
	}
	return c.client.ExecuteWorkflow(ctx, options, workflows.OrderWorkflow, input)
}

// CancelOrder sends a cancel_order signal to a running workflow.
func (c *Client) CancelOrder(ctx context.Context, workflowID string, req signals.CancelOrderRequest) error {
	return c.client.SignalWorkflow(ctx, workflowID, "", signals.CancelOrderSignal, req)
}

// ChangeShippingAddress sends an update-shipping-address signal to a running workflow.
func (c *Client) ChangeShippingAddress(ctx context.Context, workflowID string, req signals.UpdateShippingAddressRequest) error {
	return c.client.SignalWorkflow(ctx, workflowID, "", signals.UpdateShippingAddressSignal, req)
}

// ExecuteWorkflow starts a new workflow execution
func (c *Client) ExecuteWorkflow(ctx context.Context, workflowID string, workflow interface{}, args ...interface{}) (client.WorkflowRun, error) {
	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: OrderFulfillmentTaskQueue,
	}

	return c.client.ExecuteWorkflow(ctx, options, workflow, args...)
}

// SignalWorkflow sends a signal to a running workflow
func (c *Client) SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error {
	return c.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// UpdateOrderPriority sends a set_priority update to a running workflow and
// blocks until the update is accepted and applied.
func (c *Client) UpdateOrderPriority(ctx context.Context, workflowID string, input updates.SetPriorityInput) (*updates.SetPriorityResult, error) {
	handle, err := c.client.UpdateWorkflow(ctx, workflowID, "", updates.SetPriorityUpdate, input)
	if err != nil {
		return nil, fmt.Errorf("set_priority update failed for workflow %s: %w", workflowID, err)
	}

	var result updates.SetPriorityResult
	if err := handle.Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("set_priority update rejected: %w", err)
	}

	return &result, nil
}

// QueryOrderStatus queries a running workflow for the order_status query
func (c *Client) QueryOrderStatus(ctx context.Context, workflowID string) (*queries.OrderStatusResult, error) {
	resp, err := c.client.QueryWorkflow(ctx, workflowID, "", queries.OrderStatusQuery)
	if err != nil {
		return nil, fmt.Errorf("query order_status failed for workflow %s: %w", workflowID, err)
	}

	var result queries.OrderStatusResult
	if err := resp.Get(&result); err != nil {
		return nil, fmt.Errorf("failed to decode order_status response: %w", err)
	}

	return &result, nil
}

// QueryWorkflow queries a running workflow
func (c *Client) QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...interface{}) (interface{}, error) {
	resp, err := c.client.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := resp.Get(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// CancelWorkflow cancels a running workflow
func (c *Client) CancelWorkflow(ctx context.Context, workflowID string, runID string) error {
	return c.client.CancelWorkflow(ctx, workflowID, runID)
}

// GetWorkflow retrieves a workflow execution handle
func (c *Client) GetWorkflow(ctx context.Context, workflowID string, runID string) client.WorkflowRun {
	return c.client.GetWorkflow(ctx, workflowID, runID)
}

// Close closes the Temporal client connection
func (c *Client) Close() {
	if c.client != nil {
		c.client.Close()
		log.Println("Temporal client closed")
	}
}

// GetClient returns the underlying Temporal client
func (c *Client) GetClient() client.Client {
	return c.client
}
