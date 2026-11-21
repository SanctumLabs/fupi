package inmemqueue

import (
	"context"
	"github.com/sanctumlabs/curtz/app/pkg/infra/queue"
	"log/slog"

	"github.com/google/wire"
)

type inMemQueueEventPublisher struct {
	inMemQueue *InMemQueue
}

var _ InMemQueueEventPublisher = (*inMemQueueEventPublisher)(nil)

var InMemQueueEventPublisherSet = wire.NewSet(NewEventQueuePublisher)

func NewEventQueuePublisher(inMemQueue *InMemQueue) InMemQueueEventPublisher {
	return &inMemQueueEventPublisher{
		inMemQueue: inMemQueue,
	}
}

// Publish implements messaging.MessagePublisher.
func (imq *inMemQueueEventPublisher) Publish(ctx context.Context, event queue.Event) error {
	slog.InfoContext(ctx, "Publishing event", "event", event)

	imq.inMemQueue.PublishEvent(event.Topic, event)
	return nil
}

// Close implements messaging.MessagePublisher.
func (imq *inMemQueueEventPublisher) Close() error {
	slog.Error("Closing in memory message publisher")
	imq.inMemQueue.CloseEventTopics()
	return nil
}
