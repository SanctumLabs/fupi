package inmemqueue

import (
	"context"
	"log/slog"

	"github.com/sanctumlabs/fupi/app/pkg/infra/queue"

	"github.com/google/wire"
)

type inMemQueueMessagePublisher struct {
	inMemQueue *InMemQueue
}

var _ InMemQueueMessagePublisher = (*inMemQueueMessagePublisher)(nil)

var InMemQueueMessagePublisherSet = wire.NewSet(NewMessageQueuePublisher)

// NewMessageQueuePublisher initializes a new in-memory message publisher.
func NewMessageQueuePublisher(inMemQueue *InMemQueue) InMemQueueMessagePublisher {
	return &inMemQueueMessagePublisher{
		inMemQueue: inMemQueue,
	}
}

// Publish implements messaging.MessagePublisher.
func (imq *inMemQueueMessagePublisher) Publish(ctx context.Context, message queue.Message) error {
	slog.DebugContext(ctx, "Publishing message", "message", message)

	imq.inMemQueue.PublishMessage(message.Topic, message)
	return nil
}

// Close implements messaging.MessagePublisher.
func (imq *inMemQueueMessagePublisher) Close() error {
	slog.Error("Closing in memory message publisher")
	imq.inMemQueue.CloseMessageTopics()
	return nil
}
