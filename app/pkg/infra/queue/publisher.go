package queue

import "context"

// MessagePublisher handles defines the methods used to handle publication of messages to a topic on a broker
type MessagePublisher interface {
	// Publish publishes a message to a given topic
	Publish(ctx context.Context, message Message) error

	// Close closes connection to a broker
	Close() error
}

// EventPublisher handles defines the methods used to handle publication of events to a topic on a broker
type EventPublisher interface {
	// Publish publishes a message to a given topic
	Publish(ctx context.Context, event Event) error

	// Close closes connection to a broker
	Close() error
}
