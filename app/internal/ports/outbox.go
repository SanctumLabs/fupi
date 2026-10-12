package ports

import (
	"context"
	"errors"
	"time"
)

// ErrNotLeader is returned by OutboxDatastore.Acquire when another relay instance holds the lock.
var ErrNotLeader = errors.New("outbox relay: another instance is the leader")

// OutboxEvent is an event waiting in the transactional outbox (ADR-0011) to be delivered to a broker.
type OutboxEvent struct {
	// ID is the domain event's ID; consumers de-duplicate on it.
	ID string
	// Destination is the logical stream the event goes to, which is also the topic name.
	Destination string
	// PartitionKey orders and partitions the event on the broker, usually the aggregate ID. Empty when the row has none.
	PartitionKey string
	EventType    string
	// Headers are the stored message headers: event_id, event_type, aggregate_id, occurred_at and, when the writer had
	// a span, traceparent and tracestate.
	Headers map[string]string
	// Payload is the event's JSON, forwarded untouched.
	Payload []byte
	// Attempts is how many times the broker permanently rejected this event so far.
	Attempts  int
	CreatedAt time.Time
}

// Backlog describes what the relay has not delivered yet.
type Backlog struct {
	// Unsent counts events waiting for delivery (parked ones excluded).
	Unsent int64
	// OldestUnsent is when the oldest waiting event was written; the zero time when none waits.
	OldestUnsent time.Time
	// Parked counts events the relay gave up on.
	Parked int64
}

// Lease is the right to relay, held until Release or until the connection that holds it dies.
type Lease interface {
	// Alive reports whether the lease is still held, by using the connection that holds it.
	Alive(ctx context.Context) error
	// Release gives the lease up.
	Release()
}

// Leaser elects one leader among the instances that ask: Acquire takes the lease, or returns ErrNotLeader when another
// instance holds it. OutboxDatastore is one; the URL expiry job has its own, so the two elect independently.
type Leaser interface {
	Acquire(ctx context.Context) (Lease, error)
}

// OutboxDatastore is what the outbox relay needs from the database.
type OutboxDatastore interface {
	// Acquire takes the relay lease, or returns ErrNotLeader when another instance holds it.
	Acquire(ctx context.Context) (Lease, error)
	// Claim returns up to perDestination of the oldest unsent, unparked events of every destination, in creation order.
	Claim(ctx context.Context, perDestination int) ([]OutboxEvent, error)
	// MarkSent records that the events were acknowledged by the broker.
	MarkSent(ctx context.Context, ids []string) error
	// RecordRejection counts a permanent rejection of the event by the broker and returns how many it has had.
	RecordRejection(ctx context.Context, id, reason string) (attempts int, err error)
	// Park stops the relay from retrying the event; the row stays, with its reason.
	Park(ctx context.Context, id, reason string) error
	// Purge deletes up to limit events sent before olderThan and returns how many it deleted.
	Purge(ctx context.Context, olderThan time.Time, limit int) (int, error)
	// Backlog reports the undelivered and parked events.
	Backlog(ctx context.Context) (Backlog, error)
}
