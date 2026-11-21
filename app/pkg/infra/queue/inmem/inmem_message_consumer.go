package inmemqueue

import (
	"context"
	"github.com/sanctumlabs/curtz/app/pkg/infra/queue"

	"github.com/google/wire"
)

type inMemQueueMessageConsumer struct {
	workerPoolSize int
	queueName      string
	inMemQueue     *InMemQueue
}

var _ InMemQueueMessageConsumer = (*inMemQueueMessageConsumer)(nil)

var InMemQueueMessageConsumerSet = wire.NewSet(NewInMemMessageConsumer)

func NewInMemMessageConsumer(inMemQueue *InMemQueue) InMemQueueMessageConsumer {
	consumer := &inMemQueueMessageConsumer{
		inMemQueue: inMemQueue,
	}

	return consumer
}

// StartConsumer implements InMemQueueConsumer.
func (imqc *inMemQueueMessageConsumer) StartConsumer(fn queue.MessageWorker) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	message := imqc.inMemQueue.SubscribeMessage(imqc.queueName)

	go fn(ctx, message)

	// forever := make(chan bool)

	// chanErr := <-ch.NotifyClose(make(chan *error))
	// slog.Error("ch.NotifyClose", chanErr)
	// <-forever
	return nil
}
