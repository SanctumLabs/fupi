package outboxdatastore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

var _ ports.OutboxDatastore = (*Adapter)(nil)

// Adapter implements ports.OutboxDatastore on Postgres.
type Adapter struct {
	*Leaser
	queries *postgresql.Queries
	timeout time.Duration
	// leaseIdleTimeout is handed to the Leaser; see WithLeaseIdleTimeout.
	leaseIdleTimeout time.Duration
}

// outboxLeaseKey names the relay's advisory lock; leaseApplicationName tells its connection apart in pg_stat_activity.
const (
	outboxLeaseKey       = "fupi.outbox.relay"
	leaseApplicationName = "fupi-outbox-lease"
)

// Option configures an Adapter.
type Option func(*Adapter)

// WithLeaseIdleTimeout sets how long the server lets the lease connection sit idle before it drops it (and with it the
// lock). The default is a minute; a shorter time is for tests.
func WithLeaseIdleTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.leaseIdleTimeout = d }
}

// NewAdapter builds the adapter. connString is the same connection string the pool was built from; timeout bounds every
// statement, because the relay runs some of them on a context that does not carry a deadline.
func NewAdapter(client database.PostgresDatabaseClient, connString string, timeout time.Duration, opts ...Option) (*Adapter, error) {
	a := &Adapter{
		queries:          postgresql.New(client.GetDB()),
		timeout:          timeout,
		leaseIdleTimeout: DefaultLeaseIdleTimeout,
	}
	for _, opt := range opts {
		opt(a)
	}
	leaser, err := NewLeaser(connString, outboxLeaseKey, leaseApplicationName, timeout, a.leaseIdleTimeout)
	if err != nil {
		return nil, err
	}
	a.Leaser = leaser
	return a, nil
}

// clampInt32 narrows n to int32, saturating at the ends of the range instead of wrapping.
func clampInt32(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	}
	return int32(n)
}

func (a *Adapter) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.timeout)
}

// Claim returns up to perDestination of the oldest unsent, unparked events of every destination, in creation order.
func (a *Adapter) Claim(ctx context.Context, perDestination int) ([]ports.OutboxEvent, error) {
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	rows, err := a.queries.QueryClaimOutboxEvents(ctx, clampInt32(perDestination))
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	events := make([]ports.OutboxEvent, 0, len(rows))
	for _, row := range rows {
		id, idErr := postgres.UUIDToString(row.ID)
		if idErr != nil {
			return nil, fmt.Errorf("outbox event has an invalid id: %w", idErr)
		}
		events = append(events, ports.OutboxEvent{
			ID:           id,
			Destination:  row.Destination,
			PartitionKey: row.PartitionKey.String,
			EventType:    row.EventType,
			Headers:      parseHeaders(id, row.Headers),
			Payload:      row.Payload,
			Attempts:     int(row.Attempts),
			CreatedAt:    row.CreatedAt.Time,
		})
	}
	return events, nil
}

// parseHeaders reads the stored headers JSON. A row whose headers are not an object of strings must not block every
// event behind it, so it is published without headers and the problem is logged.
func parseHeaders(id string, raw []byte) map[string]string {
	headers := map[string]string{}
	if err := json.Unmarshal(raw, &headers); err != nil {
		slog.Warn("outbox event has unreadable headers, publishing it without them", "event_id", id, "error", err)
		return map[string]string{}
	}
	return headers
}

// MarkSent records that the events were acknowledged by the broker.
func (a *Adapter) MarkSent(ctx context.Context, ids []string) error {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		parsed, err := postgres.StringToUUID(id)
		if err != nil {
			return fmt.Errorf("invalid outbox event id %q: %w", id, err)
		}
		uuids = append(uuids, parsed)
	}
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	if _, err := a.queries.QueryMarkOutboxEventsSent(ctx, uuids); err != nil {
		return fmt.Errorf("mark outbox events as sent: %w", err)
	}
	return nil
}

// RecordRejection counts a permanent rejection of the event by the broker and returns how many it has had.
func (a *Adapter) RecordRejection(ctx context.Context, id, reason string) (int, error) {
	uuid, err := postgres.StringToUUID(id)
	if err != nil {
		return 0, fmt.Errorf("invalid outbox event id %q: %w", id, err)
	}
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	attempts, err := a.queries.QueryRecordOutboxEventRejection(ctx, postgresql.QueryRecordOutboxEventRejectionParams{
		ID: uuid, ErrorMessage: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("record the rejection of an outbox event: %w", err)
	}
	return int(attempts), nil
}

// Park stops the relay from retrying the event; the row stays, with its reason.
func (a *Adapter) Park(ctx context.Context, id, reason string) error {
	uuid, err := postgres.StringToUUID(id)
	if err != nil {
		return fmt.Errorf("invalid outbox event id %q: %w", id, err)
	}
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	if err := a.queries.QueryParkOutboxEvent(ctx, postgresql.QueryParkOutboxEventParams{
		ID: uuid, ErrorMessage: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		return fmt.Errorf("park an outbox event: %w", err)
	}
	return nil
}

// Purge deletes up to limit events sent before olderThan and returns how many it deleted.
func (a *Adapter) Purge(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	deleted, err := a.queries.QueryPurgeSentOutboxEvents(ctx, postgresql.QueryPurgeSentOutboxEventsParams{
		OlderThan: pgtype.Timestamptz{Time: olderThan, Valid: true},
		LimitBy:   clampInt32(limit),
	})
	if err != nil {
		return 0, fmt.Errorf("purge sent outbox events: %w", err)
	}
	return int(deleted), nil
}

// Backlog reports the undelivered and parked events.
func (a *Adapter) Backlog(ctx context.Context) (ports.Backlog, error) {
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	row, err := a.queries.QueryOutboxBacklog(ctx)
	if err != nil {
		return ports.Backlog{}, fmt.Errorf("read the outbox backlog: %w", err)
	}
	backlog := ports.Backlog{Unsent: row.Unsent, Parked: row.Parked}
	if row.OldestUnsent.Valid {
		backlog.OldestUnsent = row.OldestUnsent.Time
	}
	return backlog, nil
}
