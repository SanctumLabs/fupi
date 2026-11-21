package inmemqueue

type InMemEventConsumerOption func(*inMemQueueEventConsumer)

func QueueName(queueName string) InMemEventConsumerOption {
	return func(p *inMemQueueEventConsumer) {
		p.queueName = queueName
	}
}

func WorkerPoolSize(workerPoolSize int) InMemEventConsumerOption {
	return func(p *inMemQueueEventConsumer) {
		p.workerPoolSize = workerPoolSize
	}
}
