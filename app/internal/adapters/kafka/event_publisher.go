// Package kafkaadapter implements ports.EventPublisher on Kafka: it maps relay messages onto Kafka records and tells
// permanent failures from transient ones.
package kafkaadapter

import (
	"context"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
)

// producer is the part of kafka.Producer the adapter uses.
type producer interface {
	Produce(ctx context.Context, records []kafka.Record) []error
	Ping(ctx context.Context) error
	Close()
}

var _ ports.EventPublisher = (*EventPublisher)(nil)

// EventPublisher publishes outbox messages to Kafka.
type EventPublisher struct {
	producer producer
}

// NewEventPublisher wraps a Kafka producer.
func NewEventPublisher(p *kafka.Producer) *EventPublisher { return &EventPublisher{producer: p} }

// Publish produces the messages and returns one result per message, in order. A message the broker will never accept as it
// is (too large, an invalid topic name, ...) is a permanent failure; anything else that goes wrong is transient.
func (e *EventPublisher) Publish(ctx context.Context, messages []ports.Message) []ports.PublishResult {
	records := make([]kafka.Record, len(messages))
	for i, message := range messages {
		headers := make([]kafka.Header, len(message.Headers))
		for j, header := range message.Headers {
			headers[j] = kafka.Header{Key: header.Key, Value: header.Value}
		}
		records[i] = kafka.Record{Topic: message.Topic, Key: message.Key, Value: message.Value, Headers: headers}
	}

	errs := e.producer.Produce(ctx, records)
	results := make([]ports.PublishResult, len(messages))
	for i := range results {
		switch {
		case i >= len(errs):
			results[i] = ports.PublishResult{Err: errNoResult}
		case errs[i] != nil:
			results[i] = ports.PublishResult{Err: errs[i], Permanent: kafka.IsPermanent(errs[i])}
		}
	}
	return results
}

// Ping reports whether a broker answers.
func (e *EventPublisher) Ping(ctx context.Context) error { return e.producer.Ping(ctx) }

// Close closes the producer.
func (e *EventPublisher) Close() { e.producer.Close() }
