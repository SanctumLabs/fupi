package inmemqueue

import (
	"context"
	"github.com/sanctumlabs/curtz/app/pkg/infra/queue"

	"github.com/google/wire"
)

type inMemQueueEventConsumer struct {
	queueName      string
	workerPoolSize int
	inMemQueue     *InMemQueue
}

var _ InMemQueueEventConsumer = (*inMemQueueEventConsumer)(nil)

var InMemQueueEventConsumerSet = wire.NewSet(NewInMemEventConsumer)

func NewInMemEventConsumer(inMemQueue *InMemQueue) InMemQueueEventConsumer {
	consumer := &inMemQueueEventConsumer{
		inMemQueue: inMemQueue,
	}

	return consumer
}

// StartConsumer implements InMemQueueConsumer.
func (imqc *inMemQueueEventConsumer) StartConsumer(fn queue.EventWorker) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	event := imqc.inMemQueue.SubscribeEvent(imqc.queueName)

	go fn(ctx, event)

	// forever := make(chan bool)

	// chanErr := <-ch.NotifyClose(make(chan *error))
	// slog.Error("ch.NotifyClose", chanErr)
	// <-forever
	return nil
}
