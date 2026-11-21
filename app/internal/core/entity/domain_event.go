package entity

import "time"

// DomainEvent is raised by an aggregate when its state changes. Events are recorded on the
// aggregate and flushed to the outbox after the aggregate is persisted.
type DomainEvent interface {
	// ID is the unique identifier of this event occurrence, used for de-duplication by consumers
	ID() string

	// EventType is the stable, published name of the event (e.g. "url.created")
	EventType() string

	// OccurredAt is when the event was raised
	OccurredAt() time.Time
}
