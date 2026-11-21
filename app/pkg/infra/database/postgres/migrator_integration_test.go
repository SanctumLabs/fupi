//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Migrations are applied through Migrate, record their state in schema_migrations (the table `make migrate` and the
// compose migrate job use) and a second run changes nothing.
func TestMigrate_AppliesTheMigrationsOnceInSchemaMigrations(t *testing.T) {
	ctx := context.Background()
	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))
	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString), "a second run must be a no-op")

	pool, err := pgxpool.New(ctx, connectionString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var version int64
	var dirty bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.GreaterOrEqual(t, version, int64(1))
	assert.False(t, dirty)

	var legacyTable, outboxTable *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('bid_schema_migrations')::text").Scan(&legacyTable))
	require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('outbox_events')::text").Scan(&outboxTable))
	assert.Nil(t, legacyTable, "the old bid_schema_migrations table must not be created")
	assert.NotNil(t, outboxTable, "the migrations must have created outbox_events")
}

// The outbox relay (spec 2026-10-05) counts rejections, parks events and claims unsent rows per destination in creation
// order; the columns and the partial indexes are what it relies on.
func TestMigrate_AddsTheOutboxRelayColumnsAndIndexes(t *testing.T) {
	ctx := context.Background()
	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))

	pool, err := pgxpool.New(ctx, connectionString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var attempts, parkedAt string
	require.NoError(t, pool.QueryRow(ctx, `SELECT
		(SELECT data_type || '/' || is_nullable || '/' || column_default FROM information_schema.columns WHERE table_name = 'outbox_events' AND column_name = 'attempts'),
		(SELECT data_type || '/' || is_nullable FROM information_schema.columns WHERE table_name = 'outbox_events' AND column_name = 'parked_at')`).Scan(&attempts, &parkedAt))
	assert.Equal(t, "integer/NO/0", attempts)
	assert.Equal(t, "timestamp with time zone/YES", parkedAt)

	for index, predicate := range map[string]string{
		"ix_outbox_events_unsent_idx":          "sent_time IS NULL",
		"ix_outbox_events_sent_time_purge_idx": "sent_time IS NOT NULL",
	} {
		var definition string
		require.NoError(t, pool.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE indexname = $1", index).Scan(&definition), index)
		assert.Contains(t, definition, predicate, "%s is a partial index", index)
	}
}

func TestMigration000003_CanBeRolledBackAndAppliedAgain(t *testing.T) {
	ctx := context.Background()
	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))
	pool, err := pgxpool.New(ctx, connectionString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	const dir = "../../../../internal/adapters/postgres/migrations/"
	down, err := os.ReadFile(dir + "000003_outbox_relay.down.sql")
	require.NoError(t, err)
	up, err := os.ReadFile(dir + "000003_outbox_relay.up.sql")
	require.NoError(t, err)
	relayObjects := func() (columns, indexes int) {
		require.NoError(t, pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM information_schema.columns WHERE table_name = 'outbox_events' AND column_name IN ('attempts', 'parked_at')),
			(SELECT count(*) FROM pg_indexes WHERE indexname IN ('ix_outbox_events_unsent_idx', 'ix_outbox_events_sent_time_purge_idx'))`).Scan(&columns, &indexes))
		return columns, indexes
	}

	columns, indexes := relayObjects()
	require.Equal(t, [2]int{2, 2}, [2]int{columns, indexes}, "applied")

	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	columns, indexes = relayObjects()
	assert.Equal(t, [2]int{0, 0}, [2]int{columns, indexes}, "rolled back: no column and no index is left")

	_, err = pool.Exec(ctx, string(up))
	require.NoError(t, err)
	columns, indexes = relayObjects()
	assert.Equal(t, [2]int{2, 2}, [2]int{columns, indexes}, "applied again")
}
