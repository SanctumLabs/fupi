package url

import (
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
)

// SuspensionReason is why a URL was suspended by the Security context
type SuspensionReason string

const (
	SuspensionReasonMalware  SuspensionReason = "malware"
	SuspensionReasonPhishing SuspensionReason = "phishing"
	SuspensionReasonSpam     SuspensionReason = "spam"
)

// baseEvent carries the fields every URL domain event shares and satisfies entity.DomainEvent
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

type URLCreated struct {
	baseEvent
	URLID       string
	UserID      string
	OriginalURL string
	ShortCode   string
	ExpiresOn   time.Time
}

type URLAccessed struct {
	baseEvent
	URLID       string
	ShortCode   string
	IPAddress   string
	UserAgent   string
	Referer     string
	CountryCode string // resolved by GeoIP before publishing
	DeviceType  string // mobile | desktop | bot
}

type URLExpired struct {
	baseEvent
	URLID     string
	ShortCode string
}

type URLSuspended struct {
	baseEvent
	URLID  string
	Reason SuspensionReason
}

type URLReinstated struct {
	baseEvent
	URLID     string
	ShortCode string
}

type URLDeleted struct {
	baseEvent
	URLID     string
	ShortCode string
}
