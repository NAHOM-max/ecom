package messaging

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

// KafkaProducer implements EventProducer using the Sarama sync producer.
type KafkaProducer struct {
	producer sarama.SyncProducer
}

// KafkaConfig holds Kafka connection settings.
type KafkaConfig struct {
	Brokers []string
}

// NewKafkaProducer dials the brokers and returns a ready producer.
func NewKafkaProducer(cfg KafkaConfig) (*KafkaProducer, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(cfg.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka producer: %w", err)
	}

	log.Printf("Kafka producer connected to brokers: %v", cfg.Brokers)
	return &KafkaProducer{producer: producer}, nil
}

// Publish serialises the event to JSON and sends it to the given topic.
// The OrderID is used as the message key so all events for the same order
// land on the same partition (ordering guarantee).
func (p *KafkaProducer) Publish(topic string, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %s: %w", event.EventType, err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(event.OrderID),
		Value: sarama.ByteEncoder(body),
	}

	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to publish event %s to topic %s: %w", event.EventType, topic, err)
	}

	log.Printf("Published event type=%s order=%s topic=%s partition=%d offset=%d",
		event.EventType, event.OrderID, topic, partition, offset)
	return nil
}

// Close shuts down the underlying Sarama producer.
func (p *KafkaProducer) Close() error {
	return p.producer.Close()
}

// NoopProducer is a no-op EventProducer used in tests or when Kafka is disabled.
type NoopProducer struct{}

func (n *NoopProducer) Publish(topic string, event Event) error { return nil }
func (n *NoopProducer) Close() error                            { return nil }
