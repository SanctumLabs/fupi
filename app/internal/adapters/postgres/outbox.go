package postgresrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
)

// OutboxWriteQuerier is the SQL operation needed to append domain events to the transactional outbox.
type OutboxWriteQuerier interface {
	QueryCreateOutboxEvent(ctx context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error)
}

// outboxHeaders are the message headers a relay forwards alongside the payload.
type outboxHeaders struct {
	EventID     string    `json:"event_id"`
	EventType   string    `json:"event_type"`
	AggregateID string    `json:"aggregate_id"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// WriteOutboxEvents appends events to the outbox using q, which must be bound to the same
// transaction as the aggregate's own write so both commit or neither does. The outbox row ID is the
// event ID, so consumers can de-duplicate on it, and the aggregate ID is the partition key so one
// aggregate's events stay in order.
func WriteOutboxEvents(ctx context.Context, q OutboxWriteQuerier, destination, aggregateID string, events []entity.DomainEvent) error {
	for _, event := range events {
		eventID, idErr := postgres.StringToUUID(event.ID())
		if idErr != nil {
			return fmt.Errorf("invalid id for %s event: %w", event.EventType(), idErr)
		}

		payload, payloadErr := json.Marshal(event)
		if payloadErr != nil {
			return fmt.Errorf("failed to encode %s event payload: %w", event.EventType(), payloadErr)
		}

		headers, headersErr := json.Marshal(outboxHeaders{
			EventID:     event.ID(),
			EventType:   event.EventType(),
			AggregateID: aggregateID,
			OccurredAt:  event.OccurredAt(),
		})
		if headersErr != nil {
			return fmt.Errorf("failed to encode %s event headers: %w", event.EventType(), headersErr)
		}

		if _, createErr := q.QueryCreateOutboxEvent(ctx, postgresql.QueryCreateOutboxEventParams{
			ID:           eventID,
			PartitionKey: pgtype.Text{String: aggregateID, Valid: true},
			Destination:  destination,
			EventType:    event.EventType(),
			Headers:      headers,
			Payload:      payload,
		}); createErr != nil {
			return fmt.Errorf("failed to write %s event to outbox: %w", event.EventType(), createErr)
		}
	}

	return nil
}
