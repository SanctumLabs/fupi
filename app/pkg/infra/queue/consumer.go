package queue

import "context"

// Consumer defines a consumer that handles consumption of messages from a Broker
type Consumer interface {
	// Consumes a message from a given queue. This is mostly a blocking operation
	Consume(ctx context.Context, queue string) error

	// AddHandler adds a handler that will handle consumption of messages from a queue
	AddHandler(ctx context.Context, task string, handler func(payload []byte) error)
}

type EventWorker func(ctx context.Context, messages <-chan Event)

type MessageWorker func(ctx context.Context, messages <-chan Message)

type EventConsumer interface {
	StartConsumer(fn EventWorker) error
}

type MessageConsumer interface {
	StartConsumer(fn MessageWorker) error
}
