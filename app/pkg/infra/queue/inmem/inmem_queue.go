package inmemqueue

import (
	"github.com/sanctumlabs/curtz/app/pkg/infra/queue"
	"sync"

	"github.com/google/wire"
)

// InMemQueue manages topics and provides methods to publish and subscribe.
type InMemQueue struct {
	messageTopics map[string]chan queue.Message
	eventTopics   map[string]chan queue.Event
	mu            sync.RWMutex
	bufferSize    int
}

var InMemQueueSet = wire.NewSet(NewInMemQueue)

// NewInMemQueue initializes a new MessageQueue with a specified buffer size per topic.
func NewInMemQueue() *InMemQueue {
	return &InMemQueue{
		messageTopics: make(map[string]chan queue.Message),
		eventTopics:   make(map[string]chan queue.Event),
		bufferSize:    _defaultBufferSize, // TODO: configure this
	}
}

// Publish sends a message to the specified topic, creating the topic if it doesn't exist.
func (mq *InMemQueue) PublishMessage(topic string, msg queue.Message) {
	mq.mu.RLock()
	ch, exists := mq.messageTopics[topic]
	mq.mu.RUnlock()

	if !exists {
		mq.mu.Lock()
		// Double-check after acquiring the write lock to avoid race conditions
		ch, exists = mq.messageTopics[topic]
		if !exists {
			ch = make(chan queue.Message, mq.bufferSize)
			mq.messageTopics[topic] = ch
		}
		mq.mu.Unlock()
	}

	ch <- msg
}

// Publish sends a message to the specified topic, creating the topic if it doesn't exist.
func (mq *InMemQueue) PublishEvent(topic string, evt queue.Event) {
	mq.mu.RLock()
	ch, exists := mq.eventTopics[topic]
	mq.mu.RUnlock()

	if !exists {
		mq.mu.Lock()
		// Double-check after acquiring the write lock to avoid race conditions
		ch, exists = mq.eventTopics[topic]
		if !exists {
			ch = make(chan queue.Event, mq.bufferSize)
			mq.eventTopics[topic] = ch
		}
		mq.mu.Unlock()
	}

	ch <- evt
}

// SubscribeMessage returns a channel to receive messages from the specified topic, creating it if necessary.
func (mq *InMemQueue) SubscribeMessage(topic string) <-chan queue.Message {
	mq.mu.RLock()
	ch, exists := mq.messageTopics[topic]
	mq.mu.RUnlock()

	if !exists {
		mq.mu.Lock()
		ch, exists = mq.messageTopics[topic]
		if !exists {
			ch = make(chan queue.Message, mq.bufferSize)
			mq.messageTopics[topic] = ch
		}
		mq.mu.Unlock()
	}

	return ch
}

// Subscribe returns a channel to receive messages from the specified topic, creating it if necessary.
func (mq *InMemQueue) SubscribeEvent(topic string) <-chan queue.Event {
	mq.mu.RLock()
	ch, exists := mq.eventTopics[topic]
	mq.mu.RUnlock()

	if !exists {
		mq.mu.Lock()
		ch, exists = mq.eventTopics[topic]
		if !exists {
			ch = make(chan queue.Event, mq.bufferSize)
			mq.eventTopics[topic] = ch
		}
		mq.mu.Unlock()
	}

	return ch
}

// CloseMessageTopic safely closes a topic and removes it from the queue.
func (mq *InMemQueue) CloseMessageTopic(topic string) {

	mq.mu.Lock()
	defer mq.mu.Unlock()
	if ch, exists := mq.messageTopics[topic]; exists {
		close(ch)
		delete(mq.messageTopics, topic)
	}
}

func (mq *InMemQueue) CloseMessageTopics() {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	for _, ch := range mq.messageTopics {
		close(ch)
	}
}

// CloseEventTopic safely closes a topic and removes it from the queue.
func (mq *InMemQueue) CloseEventTopic(topic string) {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	if ch, exists := mq.eventTopics[topic]; exists {
		close(ch)
		delete(mq.eventTopics, topic)
	}
}

func (mq *InMemQueue) CloseEventTopics() {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	for _, ch := range mq.eventTopics {
		close(ch)
	}
}
