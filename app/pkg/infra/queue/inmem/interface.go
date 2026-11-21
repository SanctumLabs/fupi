package inmemqueue

import "github.com/sanctumlabs/curtz/app/pkg/infra/queue"

// publishers
type InMemQueueMessagePublisher interface {
	queue.MessagePublisher
}

type InMemQueueEventPublisher interface {
	queue.EventPublisher
}

//  consumers

type InMemQueueEventConsumer interface {
	queue.EventConsumer
}

type InMemQueueMessageConsumer interface {
	queue.MessageConsumer
}
