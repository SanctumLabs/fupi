//go:build integration

package postgresrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/require"
)

type testEvent struct {
	id string
}

func (e testEvent) ID() string            { return e.id }
func (e testEvent) EventType() string     { return "test.happened" }
func (e testEvent) OccurredAt() time.Time { return time.Now().UTC() }

// Including soft-deleted rows must not widen the unsent filter: a relay reading unsent events with
// include_deleted set must never see an event that was already sent.
func TestQueryOutboxEventsUnSent_IncludeDeletedStillExcludesSentEvents(t *testing.T) {
	ctx := context.Background()
	dbClient := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(dbClient.Close)
	queries := postgresql.New(dbClient.GetDB())

	sent := testEvent{id: entity.IDToString(entity.NewID())}
	unsent := testEvent{id: entity.IDToString(entity.NewID())}
	require.NoError(t, postgresrepo.WriteOutboxEvents(ctx, queries, "test.events", entity.IDToString(entity.NewID()), []entity.DomainEvent{sent, unsent}))

	sentID, err := postgres.StringToUUID(sent.ID())
	require.NoError(t, err)
	_, err = queries.QueryMarkOutboxEventAsSent(ctx, postgresql.QueryMarkOutboxEventAsSentParams{
		ID:       sentID,
		SentTime: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)

	rows, err := queries.QueryOutboxEventsUnSent(ctx, postgresql.QueryOutboxEventsUnSentParams{
		IncludeDeleted: true,
		OrderBy:        "created_at",
		SortOrder:      "ASC",
		LimitBy:        100,
	})
	require.NoError(t, err)

	returned := map[string]bool{}
	for _, row := range rows {
		id, idErr := postgres.UUIDToString(row.OutboxEvent.ID)
		require.NoError(t, idErr)
		returned[id] = true
	}
	require.True(t, returned[unsent.ID()], "the unsent event must be returned")
	require.False(t, returned[sent.ID()], "a sent event must never be returned as unsent")
}
