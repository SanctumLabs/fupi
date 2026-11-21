package ports

import "context"

// Header is one message header.
type Header struct {
	Key   string
	Value string
}

// Message is what the outbox relay publishes to a broker.
type Message struct {
	// Topic is the destination stream.
	Topic string
	// Key orders and partitions the message; nil lets the broker spread it.
	Key     []byte
	Value   []byte
	Headers []Header
}

// PublishResult is the outcome of publishing one message.
type PublishResult struct {
	// Err is nil when the broker acknowledged the message.
	Err error
	// Permanent means the broker will never accept this message as it is (it is too large, the topic name is invalid, ...),
	// as opposed to a failure that can go away (the broker is down, a timeout).
	Permanent bool
}

// EventPublisher publishes messages to a broker.
type EventPublisher interface {
	// Publish publishes the messages and returns one result per message, in the same order. Messages with the same key
	// reach the broker in the order given; once one of them fails, the ones after it fail too.
	Publish(ctx context.Context, messages []Message) []PublishResult
	// Ping reports whether the broker answers.
	Ping(ctx context.Context) error
	// Close releases the connection.
	Close()
}
