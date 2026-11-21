package identity

import (
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
)

// baseEvent carries the fields every Identity domain event shares and satisfies entity.DomainEvent
type baseEvent struct {
	id         string
	eventType  string
	occurredAt time.Time
}

func newBaseEvent(eventType string) baseEvent {
	return baseEvent{
		id:         entity.IDToString(entity.NewID()),
		eventType:  eventType,
		occurredAt: time.Now().UTC(),
	}
}

func (e baseEvent) ID() string            { return e.id }
func (e baseEvent) EventType() string     { return e.eventType }
func (e baseEvent) OccurredAt() time.Time { return e.occurredAt }

// UserRegistered is raised when a user registers. The Notification context consumes this to send
// the verification email; VerificationToken is carried so the consumer needs no read-back.
type UserRegistered struct {
	baseEvent
	UserID              string
	Username            string
	Email               string
	VerificationToken   string
	VerificationExpires time.Time
}

// UserVerified is raised when a user completes email verification and becomes ACTIVE.
type UserVerified struct {
	baseEvent
	UserID string
	Email  string
}

// UserDeleted is raised when a user is soft-deleted.
type UserDeleted struct {
	baseEvent
	UserID string
}
