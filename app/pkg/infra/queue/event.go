package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type (
	// Event is an event in the system. That is, something that has already occurred
	Event struct {
		// ID is the Event ID
		ID string `json:"id"`

		// EventType is the name of the event
		EventType string `json:"eventType"`

		// Topic is the topic name this Event is to be delivered on
		Topic string `json:"topic"`

		// ContentType is the content type of the Event
		ContentType string `json:"contentType"`

		// Timestamp is the time the Event was created
		Timestamp time.Time `json:"timestamp"`

		// Payload is the content of the Event to be sent
		Payload []byte `json:"payload"`
	}

	EventParams struct {
		Topic       string
		ContentType string
		Payload     []byte
	}
)

// New creates a new Event
func NewEvent(params EventParams) Event {
	id := uuid.New().String()
	timestamp := time.Now()

	return Event{
		ID:          id,
		Topic:       params.Topic,
		ContentType: params.ContentType,
		Payload:     params.Payload,
		Timestamp:   timestamp,
	}
}

// String returns a stringified version of the Event
func (m Event) String() string {
	return fmt.Sprintf("Event(id=%s, topic=%s, contentType=%s, timestamp=%s, Payload={%v})", m.ID, m.Topic, m.ContentType, m.Timestamp, m.Payload)
}

// ToBytes marshalls an event/task Event to a byte slice
func (m *Event) ToBytes() ([]byte, error) {
	eventBytes, err := json.Marshal(m)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal Event to bytes")
	}
	return eventBytes, nil
}

// ToBytes marshalls an event/task Event to a byte slice
func (m *Event) PayloadToBytes() ([]byte, error) {
	payloadBytes, err := json.Marshal(m.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal Event payload to bytes")
	}
	return payloadBytes, nil
}

// ConsumeEvent is the Event that is consumed from a queue
type ConsumerEvent struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// String returns a stringified version of the Event
func (m ConsumerEvent) String() string {
	return fmt.Sprintf("ConsumeEvent(topic=%s, Payload={%v})", m.Topic, m.Payload)
}
