package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

// HandlerFunc is called for every message received on a subscribed topic.
type HandlerFunc func(event Event) error

// KafkaConsumer subscribes to one or more topics and dispatches events to a handler.
type KafkaConsumer struct {
	group   sarama.ConsumerGroup
	topics  []string
	handler HandlerFunc
}

// NewKafkaConsumer creates a consumer group member.
func NewKafkaConsumer(cfg KafkaConfig, groupID string, topics []string, handler HandlerFunc) (*KafkaConsumer, error) {
	config := sarama.NewConfig()
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}

	group, err := sarama.NewConsumerGroup(cfg.Brokers, groupID, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	log.Printf("Kafka consumer group %q connected, topics: %v", groupID, topics)
	return &KafkaConsumer{group: group, topics: topics, handler: handler}, nil
}

// Start begins consuming in a background goroutine. Cancel ctx to stop.
func (c *KafkaConsumer) Start(ctx context.Context) {
	go func() {
		h := &consumerGroupHandler{handler: c.handler}
		for {
			if err := c.group.Consume(ctx, c.topics, h); err != nil {
				log.Printf("Kafka consumer error: %v", err)
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
}

// Close shuts down the consumer group.
func (c *KafkaConsumer) Close() error {
	return c.group.Close()
}

// consumerGroupHandler implements sarama.ConsumerGroupHandler.
type consumerGroupHandler struct {
	handler HandlerFunc
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("Failed to unmarshal event from topic %s: %v", msg.Topic, err)
			session.MarkMessage(msg, "")
			continue
		}

		if err := h.handler(event); err != nil {
			log.Printf("Handler error for event %s order=%s: %v", event.EventType, event.OrderID, err)
		}

		session.MarkMessage(msg, "")
	}
	return nil
}
