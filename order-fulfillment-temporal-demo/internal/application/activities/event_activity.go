package activities

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/infrastructure/messaging"
)

// PublishEventActivity wraps an EventProducer so workflows can emit
// Kafka events through a normal activity call — keeping the workflow
// deterministic while the side-effect (network I/O) lives here.
type PublishEventActivity struct {
	producer messaging.EventProducer
}

func NewPublishEventActivity(producer messaging.EventProducer) *PublishEventActivity {
	return &PublishEventActivity{producer: producer}
}

// PublishEventInput is the argument passed from the workflow.
type PublishEventInput struct {
	Topic string
	Event messaging.Event
}

// Publish sends the event to Kafka (or whatever EventProducer is wired in).
func (a *PublishEventActivity) Publish(ctx context.Context, input PublishEventInput) error {
	logger := activity.GetLogger(ctx)

	if err := a.producer.Publish(input.Topic, input.Event); err != nil {
		return fmt.Errorf("failed to publish event %s for order %s: %w",
			input.Event.EventType, input.Event.OrderID, err)
	}

	logger.Info("Event published",
		"eventType", input.Event.EventType,
		"orderID", input.Event.OrderID,
		"topic", input.Topic,
	)
	return nil
}
