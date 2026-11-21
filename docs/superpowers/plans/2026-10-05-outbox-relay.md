# Outbox Relay and Kafka Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the transactional outbox to Kafka: a `cmd/worker` process, one leader-elected relay that publishes `outbox_events` rows to their topics with at-least-once delivery, per-key order, parking of events Kafka permanently rejects, a purge of old sent rows and trace continuity from the request that wrote the event, plus the small franz-go producer it uses.

**Architecture:** The relay use case (`internal/application/outbox`) depends on two ports: `ports.OutboxDatastore` (Postgres adapter: purpose-built sqlc queries and an advisory-lock lease on its own connection) and `ports.EventPublisher` (Kafka adapter over a franz-go producer in `pkg/infra/queue/kafka`). The worker (`app/cmd/worker`) wires them with the slice 3 and 4 building blocks (strict config loader, slog JSON logger, OpenTelemetry, health registry) behind a small `net/http` health listener, and ships in the existing image as `/app/worker` with its own compose stack.

**Tech Stack:** Go 1.26, pgx v5 and sqlc, franz-go v1.22.1, golang-migrate, OpenTelemetry v1.44, testify, testcontainers (Postgres, and a generic container for Kafka).

**Spec:** `docs/superpowers/specs/2026-10-05-outbox-relay-design.md` (decisions D1–D14 are binding; this plan argues from it, and the amendments the planning found are already in the spec).

## Global Constraints

- Go `1.26.0` (`go.mod`). All commands run from the repo root `/Users/lusina/Projects/SanctumLabs/fupi`, on branch `feat/outbox-relay`.
- **Commit trailer (D14):** every commit made while executing this plan ends with the `Co-Authored-By` trailer given in the session's attribution instructions. The `git commit` snippets below omit the trailer text on purpose; add it to each commit.
- **Download gate:** nothing is downloaded without the user's explicit go-ahead naming source and size. franz-go v1.22.1, `pkg/kmsg` v1.14.0, `github.com/pierrec/lz4/v4` v4.1.30 and `github.com/klauspost/compress` v1.20.0 are all in the local module cache: Task 6 adds them with `GOPROXY=off`, which fails instead of downloading. Adding franz-go raises the existing indirect requirement `klauspost/compress` from v1.18.6 to v1.20.0: that is a version bump of a library in the production binary, so Task 6 asks for it. Two things do need the network and the go-ahead: `go mod tidy` (Task 6) and the Docker image build (Tasks 13 and 16: the builder runs `go mod download`).
- **No Docker pulls:** every image the plan uses is already local: `apache/kafka:4.3.1`, `postgres:16.2-alpine`, `testcontainers/ryuk`, the pinned `hadolint/hadolint@sha256:32dac94…` (use `make lint.docker`, never `docker run hadolint/hadolint:<tag>`), `prom/prometheus:v3.15.0` (for `promtool`) and the observability and ELK images. Run `docker images` before any `docker run` of an image you have not used before.
- **Docker must be running** for the integration tests and the drill (`docker info`); start Docker Desktop if it is not.
- Do not modify `.env` (the developer's own, gitignored file) or `/etc/hosts`. Never remove paths with `$(...)` or an unquoted variable in `rm`: print the path and remove the literal path.
- Environment variable names, defaults and units are exactly the spec's section 7 table. Error text from the config loader names variables and never values.
- The API process never connects to Kafka (ADR-0015); only the worker does. The worker never migrates (ADR-0014).
- Never put a payload, a message key, a header value, an email address or a token in a span or a metric label (the publish span carries the event ID, the destination and the event type only).
- Unit tests are untagged; Docker-backed tests carry `//go:build integration`. Use testify `require`/`assert`. `go test ./...` must be green at the end of every task.
- Format every Go file you create or edit with `gofmt -w` (do not reformat files you only touch elsewhere: `app/config/database.go` and others are not gofmt-clean and are out of scope).
- Before starting any Docker stack check `docker ps`: the developer's own containers must not be disturbed.

## Plan notes (where the code found while planning sharpens the spec's wording)

The code in this plan was written and run in a scratch copy of the repository first (unit tests, race detector, `golangci-lint`, and every Docker-backed test against real Postgres and Kafka containers), so the snippets are tested code; these are the things that testing taught.

1. **franz-go's ordering is guaranteed by the library** (read in its source): `RecordDeliveryTimeout` and `RecordRetries` fail every record buffered for a partition after the one that fails, and records are produced in order per partition. The per-key serial fallback the spec's first draft held in reserve is not needed (the spec says so now).
2. **A bounded `Produce` needs `AllowIdempotentProduceCancellation`.** The broker-stop test hung for more than ten minutes: by default franz-go will not fail a record whose request is already on its way, so a broker that dies mid-request keeps `ProduceSync` waiting until it returns, deadline or not. The producer allows cancellation of in-flight records and bounds every call with the publish timeout; the price is a possible duplicate, which at-least-once delivery already allows.
3. **`IsPermanent` is an explicit allowlist** (`MessageTooLarge`, `RecordListTooLarge`, `InvalidTopicException`, `InvalidRecord`, `UnsupportedForMessageFormat`, `TopicAuthorizationFailed`, `ClusterAuthorizationFailed`), not "every non-retriable `kerr`", so an unknown server error can never park an event. It lives in `pkg/infra/queue/kafka`; the adapter calls it.
4. **A client-side "record too large" needs a broker.** franz-go checks the size once it knows the topic's partitions, so the oversized-record test is an integration test.
5. **The relay waits one standby interval after losing its lease** before asking for it again, so a connection that keeps dying right after it opens cannot become a hot loop (found by a failing test).
6. **The test broker is a generic testcontainers container** with the local `apache/kafka:4.3.1` image, configured like the stack's `kafka-single` and binding a fixed host port (`app/test/test_kafka.go`). The testcontainers Kafka module is therefore not needed and not added (spec section 11 expected it).
7. **`telemetry.Start` moves out of the API's `main`.** The slice 4 helper `startTelemetry` (and its tests) becomes `telemetry.Start`, shared by the API and the worker; `telemetry.ServiceName` takes a fallback and `Options` gains `ServiceName` so the worker is `fupi-worker`. The API's behaviour is unchanged.
8. **The lease connection is parsed through `pgxpool.ParseConfig`**: `postgres.ConnectionString` carries `pool_max_conns` and `pool_min_conns`, which a plain `pgx.Connect` would send to the server as unknown settings.
9. **A permanently rejected event is retried on the next cycle**, so its attempts are used up within a few cycles (milliseconds apart), not minutes apart. Transient failures never count.
10. **Prometheus names of the relay metrics are predicted** (`outbox_relay_published_total`, `..._failures_total`, `..._publish_duration_seconds_*`, `..._oldest_unsent_age_seconds`, `..._backlog`, `..._parked_rows`, `..._leader`) from OpenTelemetry's naming; Task 16 confirms them on the running stack and fixes the alert and dashboard queries if they differ.

## Review Focus

Failure modes the spec implies but the obvious tests do not cover, most likely first. Each has a pinning test or drill in the owning task.

1. A Kafka outage must neither park events nor wedge the relay or its shutdown: events wait in the table and flow in order when Kafka returns → Task 6 (`..._ABatchThatFailsDuringAnOutageIsDeliveredInOrderAfterwards`, which hung until `AllowIdempotentProduceCancellation` was added), Task 12 (`..._DrainsTheBacklogInOrderOnceKafkaIsBack`) and the Task 16 drill.
2. Two relays at once (a leader that has not noticed it lost the lease, a failover) may duplicate but must never lose, and the standby must take over → Task 5 (lease tests) and Task 12 (`..._OnlyOneOfTwoRelaysPublishesAndTheOtherTakesOverWhenItStops`).
3. A batch in flight when the process is told to stop must be recorded as sent, not left to be published twice → Task 4 (`..._FinishesAndRecordsTheBatchInFlightWhenToldToStop`) and Task 11 (the wiring test: the stop returns without an error and frees the lock) and the Task 16 drill (SIGTERM during a batch).
4. One poison event must not stop the others, and a destination whose topic is missing must not starve the rest → Task 4 (`..._APermanentRejectionDoesNotStopTheOtherEvents`), Task 5 (claim per destination) and Task 12 (`..._ParksAnEventKafkaRejectsAndKeepsDeliveringTheRest`).
5. Polling every 100 ms must not start a trace per query, and nothing from a payload may reach a span → Task 4 (`..._RunsItsHousekeepingQueriesUnderAnUnsampledParent`, `..._PublishSpansContinueTheStoredTraceAndCarryNoPayload`).

## File Structure

| File | Responsibility |
|---|---|
| `app/internal/adapters/postgres/migrations/000003_outbox_relay.{up,down}.sql` | `attempts`, `parked_at`, the partial indexes |
| `app/internal/adapters/postgres/sql/queries/outbox/outbox_relay_queries.sql` (+ generated Go) | claim, mark sent, reject, park, purge, backlog |
| `app/internal/adapters/postgres/outbox.go` | writer stores `traceparent` and `tracestate` |
| `app/internal/ports/outbox.go`, `event_bus.go` | `OutboxDatastore`, `Lease`, `EventPublisher` and their types |
| `app/internal/application/outbox/` | the relay: cycle, leadership, parking, purge, spans, metrics |
| `app/internal/adapters/postgres/outbox/` | `OutboxDatastore` on Postgres, the advisory-lock lease |
| `app/pkg/infra/queue/kafka/producer.go` | franz-go producer wrapper, `IsPermanent` |
| `app/internal/adapters/kafka/` | `EventPublisher` on Kafka |
| `app/pkg/infra/telemetry/` | `ServiceName(fallback)`, `Options.ServiceName`, `Start` |
| `app/config/worker.go` | `LoadWorker`, `LoadKafka`, `LoadOutbox`, `LoadWorkerHealth` |
| `app/api/probes/http_handler.go` | the probes over `net/http` for the worker |
| `app/cmd/worker/` | the worker process |
| `app/test/test_kafka.go` | a single-node Kafka container for tests |
| `Dockerfile`, `deploy/worker/compose.yml`, `docker-compose.yml`, `scripts/infra.sh`, `.make/docker.mk`, `scripts/*_test.sh` | image, stack and commands |
| `deploy/observability/…` | two alert rules and the worker dashboard |
| `docs/…`, `.env.example` | documentation and ADR-0018 |

---

### Task 1: Migration 000003 and the relay queries

**Files:**
- Create: `app/internal/adapters/postgres/migrations/000003_outbox_relay.up.sql`, `000003_outbox_relay.down.sql`
- Create: `app/internal/adapters/postgres/sql/queries/outbox/outbox_relay_queries.sql`
- Modify: `app/internal/adapters/postgres/sql/schema.sql`, `app/pkg/infra/database/postgres/migrator_integration_test.go`
- Regenerate: `app/internal/adapters/postgres/sql/models.go`, `outbox_read_queries.sql.go`, `outbox_write_queries.sql.go`, and create `outbox_relay_queries.sql.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: columns `outbox_events.attempts` (int, default 0) and `parked_at` (timestamptz, null); indexes `ix_outbox_events_unsent_idx` (partial on unsent, unparked, undeleted) and `ix_outbox_events_sent_time_purge_idx`; sqlc functions `QueryClaimOutboxEvents(ctx, perDestination int32) ([]QueryClaimOutboxEventsRow, error)`, `QueryMarkOutboxEventsSent(ctx, ids []pgtype.UUID) (int64, error)`, `QueryRecordOutboxEventRejection(ctx, params) (int32, error)`, `QueryParkOutboxEvent(ctx, params) error`, `QueryPurgeSentOutboxEvents(ctx, params) (int64, error)`, `QueryOutboxBacklog(ctx) (QueryOutboxBacklogRow, error)`. Task 5 wraps them.

- [ ] **Step 1: Write the failing migration tests**

In `app/pkg/infra/database/postgres/migrator_integration_test.go` add `"os"` to the imports (keep them sorted) and append:

```go
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
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -tags integration -count=1 -run 'TestMigrate_AddsTheOutboxRelay|TestMigration000003' ./app/pkg/infra/database/postgres`
Expected: FAIL (`Received unexpected error` reading the columns, and `no such file` for the 000003 files). Docker must be running.

- [ ] **Step 3: Write the migration**

Create `app/internal/adapters/postgres/migrations/000003_outbox_relay.up.sql`:

```sql
BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Outbox relay (see docs/superpowers/specs/2026-10-05-outbox-relay-design.md).
--
-- attempts counts the times the broker permanently rejected an event; transient failures (the broker being down) are not
-- counted. parked_at is set when the relay gives up on an event after the maximum number of attempts; the row stays
-- visible with its error_message and can be re-queued with: UPDATE outbox_events SET parked_at = NULL, attempts = 0 WHERE id = '...'
--
-- The first index serves the relay's claim query (unsent, unparked, undeleted rows per destination in creation order),
-- the second its purge of old sent rows.
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
ALTER TABLE outbox_events
    ADD COLUMN attempts  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN parked_at TIMESTAMP WITH TIME ZONE NULL;

COMMENT ON COLUMN outbox_events.attempts IS 'Times the broker permanently rejected this event; transient failures are not counted';
COMMENT ON COLUMN outbox_events.parked_at IS 'When the relay gave up on this event after the maximum number of attempts; null if not parked';

CREATE INDEX ix_outbox_events_unsent_idx ON outbox_events (destination, created_at, id)
    WHERE sent_time IS NULL AND parked_at IS NULL AND deleted_at IS NULL;
CREATE INDEX ix_outbox_events_sent_time_purge_idx ON outbox_events (sent_time)
    WHERE sent_time IS NOT NULL;

COMMIT;
```

Create `app/internal/adapters/postgres/migrations/000003_outbox_relay.down.sql`:

```sql
BEGIN;

DROP INDEX IF EXISTS ix_outbox_events_sent_time_purge_idx;
DROP INDEX IF EXISTS ix_outbox_events_unsent_idx;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS parked_at,
    DROP COLUMN IF EXISTS attempts;

COMMIT;
```

- [ ] **Step 4: Run the migration tests**

Run: `go test -tags integration -count=1 -run 'TestMigrate|TestMigration000003' ./app/pkg/infra/database/postgres`
Expected: `ok` (the three tests apply the migrations, find the columns and partial indexes, and roll `000003` back and forward again).

- [ ] **Step 5: Mirror the change in `schema.sql` for sqlc**

In `app/internal/adapters/postgres/sql/schema.sql`: in `CREATE TABLE outbox_events`, between `processing_at` and `created_at` add

```sql
    attempts        INTEGER NOT NULL DEFAULT 0,
    parked_at       TIMESTAMP WITH TIME ZONE NULL,
```

after the line `COMMENT ON INDEX ix_outbox_events_created_at_event_type_idx IS ...;` add

```sql
CREATE INDEX ix_outbox_events_unsent_idx ON outbox_events (destination, created_at, id) WHERE sent_time IS NULL AND parked_at IS NULL AND deleted_at IS NULL;
CREATE INDEX ix_outbox_events_sent_time_purge_idx ON outbox_events (sent_time) WHERE sent_time IS NOT NULL;
```

and before `COMMENT ON COLUMN outbox_events.group_id IS ...` add

```sql
COMMENT ON COLUMN outbox_events.attempts IS 'Times the broker permanently rejected this event; transient failures are not counted';
COMMENT ON COLUMN outbox_events.parked_at IS 'When the relay gave up on this event after the maximum number of attempts; null if not parked';
```

- [ ] **Step 6: Write the relay queries and regenerate the Go code**

Create `app/internal/adapters/postgres/sql/queries/outbox/outbox_relay_queries.sql`:

```sql
-- name: QueryClaimOutboxEvents :many
-- The relay's batch: per destination, the oldest unsent, unparked, undeleted events in creation order, so a destination
-- whose topic is unavailable cannot starve the others. The creation order is (created_at, id); id is a UUIDv7 and keeps
-- the events of one transaction in the order they were recorded.
SELECT
  ranked.id,
  ranked.partition_key,
  ranked.destination,
  ranked.event_type,
  ranked.headers,
  ranked.payload,
  ranked.attempts,
  ranked.created_at
FROM (
  SELECT
    oe.*,
    row_number() OVER (PARTITION BY oe.destination ORDER BY oe.created_at, oe.id) AS position
  FROM outbox_events oe
  WHERE oe.sent_time IS NULL
    AND oe.parked_at IS NULL
    AND oe.deleted_at IS NULL
) ranked
WHERE ranked.position <= sqlc.arg(per_destination)::int
ORDER BY ranked.created_at, ranked.id;

-- name: QueryMarkOutboxEventsSent :execrows
UPDATE outbox_events
SET
  sent_time = now(),
  updated_at = now()
WHERE id = ANY(sqlc.arg(ids)::uuid[])
  AND sent_time IS NULL;

-- name: QueryRecordOutboxEventRejection :one
UPDATE outbox_events
SET
  attempts = attempts + 1,
  error_message = sqlc.arg(error_message),
  updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING attempts;

-- name: QueryParkOutboxEvent :exec
UPDATE outbox_events
SET
  parked_at = now(),
  error_message = sqlc.arg(error_message),
  updated_at = now()
WHERE id = sqlc.arg(id);

-- name: QueryPurgeSentOutboxEvents :execrows
DELETE FROM outbox_events
WHERE id IN (
  SELECT oe.id
  FROM outbox_events oe
  WHERE oe.sent_time IS NOT NULL
    AND oe.sent_time < sqlc.arg(older_than)::timestamptz
  ORDER BY oe.sent_time
  LIMIT sqlc.arg(limit_by)::int
);

-- name: QueryOutboxBacklog :one
SELECT
  count(*) FILTER (WHERE parked_at IS NULL)::bigint AS unsent,
  (min(created_at) FILTER (WHERE parked_at IS NULL))::timestamptz AS oldest_unsent,
  count(*) FILTER (WHERE parked_at IS NOT NULL)::bigint AS parked
FROM outbox_events
WHERE sent_time IS NULL
  AND deleted_at IS NULL;
```

Run:

```bash
~/go/bin/sqlc version        # expect v1.31.1, the version in the headers of the generated files
~/go/bin/sqlc generate
git status --short app/internal/adapters/postgres/sql
go build ./...
```

Expected: `v1.31.1`; changes only in `models.go` (two new fields on `OutboxEvent`), `outbox_read_queries.sql.go` and `outbox_write_queries.sql.go` (their `Scan` lists gain the two columns), `schema.sql`, and the new files `outbox_relay_queries.sql` and `outbox_relay_queries.sql.go`; the build is clean. If sqlc reports another version or rewrites unrelated generated files, stop and ask.

- [ ] **Step 7: Run the suites that touch the outbox**

Run: `go test ./... && go test -tags integration -count=1 ./app/internal/adapters/postgres/...`
Expected: all `ok` (the existing outbox integration test still passes with the two extra columns).

- [ ] **Step 8: Commit**

```bash
git add app/internal/adapters/postgres app/pkg/infra/database/postgres
git commit -m "feat(outbox): add the relay columns, indexes and queries"
```

---

### Task 2: The writer stores the request's trace context

**Files:**
- Modify: `app/internal/adapters/postgres/outbox.go`
- Create: `app/internal/adapters/postgres/outbox_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `WriteOutboxEvents` adds `traceparent` (and `tracestate` when set) to the stored `headers` when the context carries a valid span context, using `propagation.TraceContext{}` directly (never baggage). Task 4's relay reads them from the claimed event's `Headers`.

- [ ] **Step 1: Write the failing tests**

Create `app/internal/adapters/postgres/outbox_test.go`:

```go
package postgresrepo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

type recordingOutboxQuerier struct {
	params []postgresql.QueryCreateOutboxEventParams
}

func (q *recordingOutboxQuerier) QueryCreateOutboxEvent(_ context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error) {
	q.params = append(q.params, params)
	return postgresql.OutboxEvent{}, nil
}

type fixedEvent struct{ id string }

func (e fixedEvent) ID() string            { return e.id }
func (e fixedEvent) EventType() string     { return "user.registered" }
func (e fixedEvent) OccurredAt() time.Time { return time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) }

func writtenHeaders(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	q := &recordingOutboxQuerier{}
	id := entity.IDToString(entity.NewID())
	require.NoError(t, WriteOutboxEvents(ctx, q, "identity.events", "user-1", []entity.DomainEvent{fixedEvent{id: id}}))
	require.Len(t, q.params, 1)

	var headers map[string]string
	require.NoError(t, json.Unmarshal(q.params[0].Headers, &headers))
	return headers
}

func spanContextOf(t *testing.T, flags trace.TraceFlags, state string) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	traceState, err := trace.ParseTraceState(state)
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: flags, TraceState: traceState,
	}))
}

func TestWriteOutboxEvents_StoresTheTraceContextOfTheRequestThatWroteTheEvent(t *testing.T) {
	headers := writtenHeaders(t, spanContextOf(t, trace.FlagsSampled, "vendor=value"))

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", headers["traceparent"])
	assert.Equal(t, "vendor=value", headers["tracestate"])
	assert.Equal(t, "user-1", headers["aggregate_id"], "the existing headers are still written")
	assert.Equal(t, "user.registered", headers["event_type"])
}

func TestWriteOutboxEvents_AnUnsampledSpanIsStoredWithItsFlagSoTheRelayStaysUnsampled(t *testing.T) {
	headers := writtenHeaders(t, spanContextOf(t, 0, ""))

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00", headers["traceparent"])
	assert.NotContains(t, headers, "tracestate", "no tracestate when the span has none")
}

func TestWriteOutboxEvents_WithoutASpanThereIsNoTraceHeader(t *testing.T) {
	headers := writtenHeaders(t, context.Background())

	assert.NotContains(t, headers, "traceparent")
	assert.NotContains(t, headers, "tracestate")
}

// Baggage can carry user data and the outbox row outlives the request, so it is never stored.
func TestWriteOutboxEvents_NeverStoresBaggage(t *testing.T) {
	member, err := baggage.NewMember("user", "jane@example.com")
	require.NoError(t, err)
	bag, err := baggage.New(member)
	require.NoError(t, err)
	ctx := baggage.ContextWithBaggage(spanContextOf(t, trace.FlagsSampled, ""), bag)

	headers := writtenHeaders(t, ctx)

	assert.NotContains(t, headers, "baggage")
	for _, value := range headers {
		assert.NotContains(t, value, "jane@example.com")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 -run TestWriteOutboxEvents ./app/internal/adapters/postgres`
Expected: FAIL `TestWriteOutboxEvents_StoresTheTraceContextOfTheRequestThatWroteTheEvent` and `..._AnUnsampledSpanIsStoredWithItsFlagSoTheRelayStaysUnsampled` (no `traceparent` header); the other two pass.

- [ ] **Step 3: Implement**

In `app/internal/adapters/postgres/outbox.go` add `"go.opentelemetry.io/otel/propagation"` to the imports (third-party group, after the fupi imports), replace the `outboxHeaders` type with

```go
// outboxHeaders are the message headers a relay forwards alongside the payload. TraceParent and TraceState are the W3C
// trace context of the request that wrote the event, so the relay can continue its trace; they are absent when the
// writer had no span.
type outboxHeaders struct {
	EventID     string    `json:"event_id"`
	EventType   string    `json:"event_type"`
	AggregateID string    `json:"aggregate_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	TraceParent string    `json:"traceparent,omitempty"`
	TraceState  string    `json:"tracestate,omitempty"`
}
```

add above `WriteOutboxEvents`

```go
// traceHeaders returns the traceparent and tracestate of the span in ctx. It uses the W3C trace-context propagator
// directly rather than the global one, so baggage, which can carry user data, is never stored with an event.
func traceHeaders(ctx context.Context) (traceParent, traceState string) {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent"), carrier.Get("tracestate")
}
```

in `WriteOutboxEvents` add `traceParent, traceState := traceHeaders(ctx)` as the first line before the `for` loop, and add `TraceParent: traceParent,` and `TraceState: traceState,` to the `outboxHeaders{...}` literal after `OccurredAt`.

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/internal/adapters/postgres; go test -race -count=1 ./app/internal/adapters/postgres`
Expected: no gofmt output; `ok`.

- [ ] **Step 5: Commit**

```bash
git add app/internal/adapters/postgres
git commit -m "feat(outbox): store the trace context of the writing request in the event headers"
```

---

### Task 3: The ports and message building

**Files:**
- Create: `app/internal/ports/outbox.go`
- Modify: `app/internal/ports/event_bus.go` (currently only `package ports`)
- Create: `app/internal/application/outbox/message.go`, `message_test.go`

**Interfaces:**
- Consumes: Task 2's stored headers (read as a map).
- Produces: `ports.OutboxEvent`, `ports.Backlog`, `ports.Lease`, `ports.OutboxDatastore`, `ports.ErrNotLeader`, `ports.Header`, `ports.Message`, `ports.PublishResult`, `ports.EventPublisher`; in package `outbox`: `buildMessage(spanCtx context.Context, event ports.OutboxEvent) ports.Message`, `storedTraceContext(ctx, event) context.Context`, `mapCarrier`. Tasks 4, 5 and 7 use them.

- [ ] **Step 1: Write the failing tests**

Create `app/internal/application/outbox/message_test.go` (it also holds the `event` and `headerMap` helpers the later relay tests use):

```go
package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func event(id, destination, key string) ports.OutboxEvent {
	return ports.OutboxEvent{
		ID: id, Destination: destination, PartitionKey: key, EventType: "user.registered",
		Headers:   map[string]string{"event_id": id, "event_type": "user.registered", "aggregate_id": key},
		Payload:   []byte(`{"id":"` + id + `"}`),
		CreatedAt: time.Unix(1_700_000_000, 0),
	}
}

func headerMap(headers []ports.Header) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		out[h.Key] = h.Value
	}
	return out
}

func TestBuildMessage_MapsTheRowOntoATopicKeyValueAndHeaders(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["occurred_at"] = "2026-10-05T10:00:00Z"

	msg := buildMessage(context.Background(), e)

	assert.Equal(t, "identity.events", msg.Topic)
	assert.Equal(t, []byte("user-1"), msg.Key)
	assert.Equal(t, e.Payload, msg.Value, "the payload is forwarded untouched")
	assert.Equal(t, map[string]string{
		"event_id": "e1", "event_type": "user.registered", "aggregate_id": "user-1",
		"occurred_at": "2026-10-05T10:00:00Z", "content-type": "application/json",
	}, headerMap(msg.Headers))
}

func TestBuildMessage_HeadersAreInKeyOrderSoTheOutputIsStable(t *testing.T) {
	msg := buildMessage(context.Background(), event("e1", "identity.events", "user-1"))

	var keys []string
	for _, h := range msg.Headers {
		keys = append(keys, h.Key)
	}
	assert.IsIncreasing(t, keys)
}

func TestBuildMessage_ARowWithoutAPartitionKeyHasANilKey(t *testing.T) {
	msg := buildMessage(context.Background(), event("e1", "identity.events", ""))

	assert.Nil(t, msg.Key, "an empty key must not become a zero-length key: Kafka would hash it to one partition")
}

func publishSpanContext(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
}

func TestBuildMessage_SendsThePublishSpansTraceContextNotTheStoredOne(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	e.Headers["tracestate"] = "vendor=stored"

	msg := buildMessage(publishSpanContext(t), e)

	headers := headerMap(msg.Headers)
	assert.Equal(t, "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", headers["traceparent"])
	assert.NotContains(t, headers, "tracestate", "the stored tracestate belongs to the stored span, not to the publish span")
}

func TestBuildMessage_ForwardsTheStoredTraceContextWhenThereIsNoPublishSpan(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	msg := buildMessage(context.Background(), e)

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", headerMap(msg.Headers)["traceparent"],
		"with tracing off the consumer can still continue the request's trace")
}

func TestBuildMessage_DoesNotChangeTheStoredHeaders(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	buildMessage(publishSpanContext(t), e)

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", e.Headers["traceparent"])
	assert.NotContains(t, e.Headers, "content-type")
}

func TestStoredTraceContext_ParsesTheWritersTraceparentAndIgnoresGarbage(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sc := trace.SpanContextFromContext(storedTraceContext(context.Background(), e))
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", sc.TraceID().String())
	assert.Equal(t, "00f067aa0ba902b7", sc.SpanID().String())
	assert.True(t, sc.IsRemote())

	e.Headers["traceparent"] = "not a traceparent"
	assert.False(t, trace.SpanContextFromContext(storedTraceContext(context.Background(), e)).IsValid())

	delete(e.Headers, "traceparent")
	assert.False(t, trace.SpanContextFromContext(storedTraceContext(context.Background(), e)).IsValid())
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/internal/application/outbox`
Expected: build failure: the package has no non-test Go files, `undefined: buildMessage`, `undefined: ports.OutboxEvent` (and `ports.Message`).

- [ ] **Step 3: Write the ports**

Create `app/internal/ports/outbox.go`:

```go
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
```

Replace the whole of `app/internal/ports/event_bus.go` with:

```go
package ports

import "context"

// Header is one message header.
type Header struct {
	Key   string
	Value string
}

// Message is what the outbox relay publishes to a broker.
type Message struct {
	// Topic is the destination stream.
	Topic string
	// Key orders and partitions the message; nil lets the broker spread it.
	Key     []byte
	Value   []byte
	Headers []Header
}

// PublishResult is the outcome of publishing one message.
type PublishResult struct {
	// Err is nil when the broker acknowledged the message.
	Err error
	// Permanent means the broker will never accept this message as it is (it is too large, the topic name is invalid, ...),
	// as opposed to a failure that can go away (the broker is down, a timeout).
	Permanent bool
}

// EventPublisher publishes messages to a broker.
type EventPublisher interface {
	// Publish publishes the messages and returns one result per message, in the same order. Messages with the same key
	// reach the broker in the order given; once one of them fails, the ones after it fail too.
	Publish(ctx context.Context, messages []Message) []PublishResult
	// Ping reports whether the broker answers.
	Ping(ctx context.Context) error
	// Close releases the connection.
	Close()
}
```

- [ ] **Step 4: Write the message building**

Create `app/internal/application/outbox/message.go`:

```go
package outbox

import (
	"context"
	"sort"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	headerTraceParent = "traceparent"
	headerTraceState  = "tracestate"
	contentTypeJSON   = "application/json"
)

// mapCarrier lets the W3C propagator read and write the trace headers of a stored event.
type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string { return c[key] }
func (c mapCarrier) Set(key, value string) { c[key] = value }
func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

// storedTraceContext returns ctx carrying the trace context the writer stored with the event, so the publish span is
// a child of the request that wrote it. Without a stored context, or with a malformed one, ctx is returned unchanged.
func storedTraceContext(ctx context.Context, event ports.OutboxEvent) context.Context {
	return propagation.TraceContext{}.Extract(ctx, mapCarrier(event.Headers))
}

// buildMessage turns a claimed event into a broker message: the destination is the topic, the partition key the key, the
// payload the value untouched, and the stored headers are forwarded in key order plus a content type. The trace headers
// are the publish span's, so a consumer's span is a child of it; if there is no valid span (tracing is off) the stored
// ones are forwarded as they are.
func buildMessage(spanCtx context.Context, event ports.OutboxEvent) ports.Message {
	headers := make(map[string]string, len(event.Headers)+1)
	for key, value := range event.Headers {
		headers[key] = value
	}
	if trace.SpanContextFromContext(spanCtx).IsValid() {
		delete(headers, headerTraceParent)
		delete(headers, headerTraceState)
		propagation.TraceContext{}.Inject(spanCtx, mapCarrier(headers))
	}
	headers["content-type"] = contentTypeJSON

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]ports.Header, 0, len(keys))
	for _, key := range keys {
		list = append(list, ports.Header{Key: key, Value: headers[key]})
	}

	var key []byte
	if event.PartitionKey != "" {
		key = []byte(event.PartitionKey)
	}
	return ports.Message{Topic: event.Destination, Key: key, Value: event.Payload, Headers: list}
}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l app/internal/ports app/internal/application/outbox; go vet ./app/internal/... && go test -race -count=1 ./app/internal/application/outbox`
Expected: no gofmt output; vet clean; `ok`.

- [ ] **Step 6: Commit**

```bash
git add app/internal/ports app/internal/application/outbox
git commit -m "feat(outbox): add the outbox and event bus ports and the message building"
```

---

### Task 4: The relay

**Files:**
- Create: `app/internal/application/outbox/relay.go`, `metrics.go`
- Create: `app/internal/application/outbox/fakes_test.go`, `relay_test.go`, `relay_observability_test.go`

**Interfaces:**
- Consumes: `ports.*` and `buildMessage` (Task 3); `telemetry.Unsampled` (slice 4).
- Produces: `outbox.Config{PollInterval, BatchSize, MaxAttempts, StandbyInterval, Retention, BacklogInterval, PurgeInterval, PurgeBatch, BackoffMin, BackoffMax}`; `outbox.NewRelay(store ports.OutboxDatastore, publisher ports.EventPublisher, cfg Config, opts ...Option) (*Relay, error)`; `(*Relay).Run(ctx) error` (returns nil when ctx ends); `(*Relay).Healthy(maxAge time.Duration) bool`; `outbox.WithTracerProvider`, `outbox.WithMeterProvider`. Tasks 11 and 12 use them.

- [ ] **Step 1: Write the failing tests**

Create `app/internal/application/outbox/fakes_test.go` (scripted in-memory `OutboxDatastore` and `EventPublisher`):

```go
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
)

// fakeStore is an in-memory ports.OutboxDatastore. Its behaviour is scripted by the fields a test sets.
type fakeStore struct {
	acquireErrs []error // one per Acquire call; later calls succeed
	aliveErrs   []error // one per Alive call across all leases; later calls succeed
	claims      [][]ports.OutboxEvent
	claimErr    error
	markErr     error
	purgeCounts []int // rows each Purge call reports deleting; later calls delete nothing
	backlog     ports.Backlog

	acquires   int
	claimCalls int
	released   int
	markSent   [][]string
	attempts   map[string]int
	rejections []string
	parked     []string
	purges     []purgeCall
	housekeep  []context.Context // contexts the relay used for its housekeeping queries
}

type purgeCall struct {
	olderThan time.Time
	limit     int
}

func (s *fakeStore) Acquire(ctx context.Context) (ports.Lease, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.acquires++
	if len(s.acquireErrs) > 0 {
		err := s.acquireErrs[0]
		s.acquireErrs = s.acquireErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &fakeLease{store: s}, nil
}

func (s *fakeStore) Claim(ctx context.Context, perDestination int) ([]ports.OutboxEvent, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.claimCalls++
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if len(s.claims) == 0 {
		return nil, nil
	}
	batch := s.claims[0]
	s.claims = s.claims[1:]
	return batch, nil
}

func (s *fakeStore) MarkSent(ctx context.Context, ids []string) error {
	s.housekeep = append(s.housekeep, ctx)
	if s.markErr != nil {
		return s.markErr
	}
	s.markSent = append(s.markSent, append([]string(nil), ids...))
	return nil
}

func (s *fakeStore) RecordRejection(ctx context.Context, id, reason string) (int, error) {
	s.housekeep = append(s.housekeep, ctx)
	if s.attempts == nil {
		s.attempts = map[string]int{}
	}
	s.attempts[id]++
	s.rejections = append(s.rejections, id)
	return s.attempts[id], nil
}

func (s *fakeStore) Park(ctx context.Context, id, reason string) error {
	s.housekeep = append(s.housekeep, ctx)
	s.parked = append(s.parked, id)
	return nil
}

func (s *fakeStore) Purge(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.purges = append(s.purges, purgeCall{olderThan: olderThan, limit: limit})
	if len(s.purgeCounts) == 0 {
		return 0, nil
	}
	deleted := s.purgeCounts[0]
	s.purgeCounts = s.purgeCounts[1:]
	return deleted, nil
}

func (s *fakeStore) Backlog(ctx context.Context) (ports.Backlog, error) {
	s.housekeep = append(s.housekeep, ctx)
	return s.backlog, nil
}

type fakeLease struct{ store *fakeStore }

func (l *fakeLease) Alive(ctx context.Context) error {
	l.store.housekeep = append(l.store.housekeep, ctx)
	if len(l.store.aliveErrs) == 0 {
		return nil
	}
	err := l.store.aliveErrs[0]
	l.store.aliveErrs = l.store.aliveErrs[1:]
	return err
}

func (l *fakeLease) Release() { l.store.released++ }

// fakePublisher is a scripted ports.EventPublisher.
type fakePublisher struct {
	// publish decides the results; nil acknowledges everything.
	publish func(call int, messages []ports.Message) []ports.PublishResult

	calls    [][]ports.Message
	contexts []context.Context
}

func (p *fakePublisher) Publish(ctx context.Context, messages []ports.Message) []ports.PublishResult {
	p.contexts = append(p.contexts, ctx)
	p.calls = append(p.calls, append([]ports.Message(nil), messages...))
	if p.publish != nil {
		return p.publish(len(p.calls), messages)
	}
	return acked(len(messages))
}

func (p *fakePublisher) Ping(context.Context) error { return nil }
func (p *fakePublisher) Close()                     {}

func acked(n int) []ports.PublishResult { return make([]ports.PublishResult, n) }

var (
	errTransient = errors.New("broker not available")
	errPermanent = errors.New("message too large")
)

func transient() ports.PublishResult { return ports.PublishResult{Err: errTransient} }
func permanent() ports.PublishResult { return ports.PublishResult{Err: errPermanent, Permanent: true} }
```

Create `app/internal/application/outbox/relay_test.go` (the cycle: acknowledged ids only, immediate next cycle after progress, backoff and its reset, parking at the limit, poison not blocking the rest, standby, lease loss, claim failure, unmarkable batch, wrong result count, purge cadence, finishing the batch in flight at shutdown, unsampled housekeeping, liveness):

```go
package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func testConfig() Config {
	return Config{
		PollInterval:    100 * time.Millisecond,
		BatchSize:       100,
		MaxAttempts:     3,
		StandbyInterval: 5 * time.Second,
		Retention:       0,
		BacklogInterval: 5 * time.Second,
		PurgeInterval:   10 * time.Minute,
		PurgeBatch:      1000,
		BackoffMin:      200 * time.Millisecond,
		BackoffMax:      time.Second,
	}
}

// harness runs a relay on a fake clock whose sleeps are recorded; the run ends after stopAfter sleeps.
type harness struct {
	relay  *Relay
	store  *fakeStore
	pub    *fakePublisher
	sleeps []time.Duration
	clock  time.Time
	ctx    context.Context
	cancel context.CancelFunc
}

func newHarness(t *testing.T, store *fakeStore, pub *fakePublisher, cfg Config, stopAfter int, opts ...Option) *harness {
	t.Helper()
	relay, err := NewRelay(store, pub, cfg, opts...)
	require.NoError(t, err)

	h := &harness{relay: relay, store: store, pub: pub, clock: time.Now()}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	t.Cleanup(h.cancel)
	relay.now = func() time.Time { return h.clock }
	relay.sleep = func(_ context.Context, d time.Duration) {
		h.sleeps = append(h.sleeps, d)
		h.clock = h.clock.Add(d)
		if len(h.sleeps) >= stopAfter {
			h.cancel()
		}
	}
	return h
}

func (h *harness) run(t *testing.T) {
	t.Helper()
	require.NoError(t, h.relay.Run(h.ctx))
}

func TestRelay_MarksOnlyTheAcknowledgedEvents(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("e1", "identity.events", "u1"), event("e2", "identity.events", "u2"), event("e3", "identity.events", "u3")}}}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult {
		return []ports.PublishResult{{}, transient(), {}}
	}}
	h := newHarness(t, store, pub, testConfig(), 2)

	h.run(t)

	assert.Equal(t, [][]string{{"e1", "e3"}}, store.markSent, "e2 stays unsent and is claimed again later")
	assert.Empty(t, store.rejections, "a transient failure is not a rejection")
	assert.Empty(t, store.parked)
}

func TestRelay_RunsTheNextCycleAtOnceAfterProgressAndWaitsOnlyWhenNothingIsClaimed(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}, {event("e2", "identity.events", "u2")}}}
	pub := &fakePublisher{}
	h := newHarness(t, store, pub, testConfig(), 1)

	h.run(t)

	assert.Len(t, pub.calls, 2, "both batches were published before the first wait")
	assert.Equal(t, []time.Duration{100 * time.Millisecond}, h.sleeps, "the only wait is the poll interval of the empty claim")
	assert.Equal(t, [][]string{{"e1"}, {"e2"}}, store.markSent)
}

func TestRelay_BacksOffWhileNothingIsAcknowledgedAndStartsOverAfterProgress(t *testing.T) {
	failing := event("e1", "identity.events", "u1")
	store := &fakeStore{claims: [][]ports.OutboxEvent{{failing}, {failing}, {failing}, {failing}, {failing}, {failing}, {failing}}}
	pub := &fakePublisher{publish: func(call int, _ []ports.Message) []ports.PublishResult {
		if call == 6 {
			return acked(1)
		}
		return []ports.PublishResult{transient()}
	}}
	h := newHarness(t, store, pub, testConfig(), 7)

	h.run(t)

	assert.Equal(t, []time.Duration{
		200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second, time.Second, // doubling up to the cap
		// the sixth publish succeeded: the backoff resets and the next claim (e1 again, failing) waits 200 ms, not a second
		200 * time.Millisecond,
		// then the claims run out
		100 * time.Millisecond,
	}, h.sleeps)
}

func TestRelay_ParksAnEventOnlyAfterTheMaximumPermanentRejections(t *testing.T) {
	poison := event("bad", "identity.events", "u1")
	store := &fakeStore{claims: [][]ports.OutboxEvent{{poison}, {poison}, {poison}}}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult { return []ports.PublishResult{permanent()} }}
	h := newHarness(t, store, pub, testConfig(), 4)

	h.run(t)

	assert.Equal(t, []string{"bad", "bad", "bad"}, store.rejections)
	assert.Equal(t, []string{"bad"}, store.parked, "parked on the third rejection, not before")
	assert.Empty(t, store.markSent)
}

func TestRelay_APermanentRejectionDoesNotStopTheOtherEvents(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("bad", "identity.events", "u1"), event("good", "identity.events", "u2")}}}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult {
		return []ports.PublishResult{permanent(), {}}
	}}
	h := newHarness(t, store, pub, testConfig(), 2)

	h.run(t)

	assert.Equal(t, [][]string{{"good"}}, store.markSent)
	assert.Equal(t, []string{"bad"}, store.rejections)
}

func TestRelay_AStandbyDoesNotRelayAndRetriesEveryStandbyInterval(t *testing.T) {
	store := &fakeStore{
		acquireErrs: []error{ports.ErrNotLeader, ports.ErrNotLeader},
		claims:      [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}},
	}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 3)

	h.run(t)

	assert.Equal(t, 3, store.acquires, "two refusals, then the lease")
	assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second, 100 * time.Millisecond}, h.sleeps)
	assert.Equal(t, [][]string{{"e1"}}, store.markSent, "it relayed only once it was the leader")
	assert.Equal(t, 1, store.released)
}

func TestRelay_StopsPublishingWhenTheLeaseIsLostAndWaitsBeforeAskingAgain(t *testing.T) {
	store := &fakeStore{
		aliveErrs: []error{nil, errors.New("connection closed")},
		claims:    [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}, {event("e2", "identity.events", "u2")}},
	}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 1)

	h.run(t)

	assert.Equal(t, [][]string{{"e1"}}, store.markSent, "e2 was never claimed: the second cycle found the lease dead")
	assert.Equal(t, 1, store.acquires, "it did not ask again before the standby interval passed")
	assert.Equal(t, 1, store.released, "the dead lease was released")
	assert.Equal(t, []time.Duration{5 * time.Second}, h.sleeps)
}

func TestRelay_ATemporaryClaimFailureBacksOffAndTheRelayCarriesOn(t *testing.T) {
	store := &fakeStore{claimErr: errors.New("database down")}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 3)

	h.run(t)

	assert.Equal(t, []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}, h.sleeps)
	assert.Equal(t, 3, store.claimCalls)
}

func TestRelay_PublishedEventsThatCannotBeMarkedAreNotCountedAsProgress(t *testing.T) {
	store := &fakeStore{
		claims:  [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}},
		markErr: errors.New("database down"),
	}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 1)

	h.run(t)

	assert.Equal(t, []time.Duration{200 * time.Millisecond}, h.sleeps, "the backoff, not the poll interval: the database is in trouble")
}

func TestRelay_AWrongNumberOfResultsIsATransientFailureOfEveryEvent(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("e1", "identity.events", "u1"), event("e2", "identity.events", "u2")}}}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult { return acked(1) }}
	h := newHarness(t, store, pub, testConfig(), 1)

	h.run(t)

	assert.Empty(t, store.markSent, "nothing is marked when the publisher's answer cannot be matched to the events")
	assert.Empty(t, store.rejections)
}

func TestRelay_PurgesOnItsCadenceInFullBatchesAndOnlyWithARetention(t *testing.T) {
	cfg := testConfig()
	cfg.Retention = 7 * 24 * time.Hour
	cfg.PollInterval = 6 * time.Minute
	cfg.PurgeBatch = 1000
	store := &fakeStore{purgeCounts: []int{1000, 1000, 3}}
	h := newHarness(t, store, &fakePublisher{}, cfg, 3)
	start := h.clock

	h.run(t)

	require.Len(t, store.purges, 4, "at t=0 a purge of three calls (the last batch is not full), at t=12m one more that finds nothing")
	assert.Equal(t, 1000, store.purges[0].limit)
	assert.WithinDuration(t, start.Add(-7*24*time.Hour), store.purges[0].olderThan, time.Second)
	assert.WithinDuration(t, start.Add(12*time.Minute-7*24*time.Hour), store.purges[3].olderThan, time.Second, "the cutoff moves with the clock")

	off := newHarness(t, &fakeStore{}, &fakePublisher{}, func() Config { c := testConfig(); c.PollInterval = 6 * time.Minute; return c }(), 3)
	off.run(t)
	assert.Empty(t, off.store.purges, "a zero retention never purges")
}

func TestRelay_FinishesAndRecordsTheBatchInFlightWhenToldToStop(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}}}
	var h *harness
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult {
		h.cancel() // the SIGTERM arrives while the batch is on its way to the broker
		return acked(1)
	}}
	h = newHarness(t, store, pub, testConfig(), 100)

	h.run(t)

	assert.Equal(t, [][]string{{"e1"}}, store.markSent, "the acknowledged event is recorded as sent, not left to be published twice")
	require.Len(t, pub.contexts, 1)
	assert.NoError(t, pub.contexts[0].Err(), "the publish context survives the shutdown signal")
	assert.Empty(t, h.sleeps, "and the relay stops without waiting again")
}

func TestRelay_RunsItsHousekeepingQueriesUnderAnUnsampledParent(t *testing.T) {
	store := &fakeStore{claims: [][]ports.OutboxEvent{{event("e1", "identity.events", "u1")}}, backlog: ports.Backlog{Unsent: 1}}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 1)

	h.run(t)

	require.NotEmpty(t, store.housekeep)
	for _, ctx := range store.housekeep {
		sc := trace.SpanContextFromContext(ctx)
		assert.True(t, sc.IsValid(), "a polling relay must not start a trace per query")
		assert.False(t, sc.IsSampled())
	}
}

func TestRelay_HealthyUntilTheLoopHasBeenSilentTooLong(t *testing.T) {
	h := newHarness(t, &fakeStore{}, &fakePublisher{}, testConfig(), 1)
	assert.True(t, h.relay.Healthy(30*time.Second))

	h.clock = h.clock.Add(31 * time.Second)
	assert.False(t, h.relay.Healthy(30*time.Second))

	h.run(t)
	assert.True(t, h.relay.Healthy(30*time.Second), "a completed cycle is a heartbeat")
}
```

Create `app/internal/application/outbox/relay_observability_test.go` (publish spans continue the stored trace and carry no payload; the metrics):

```go
package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRelay_PublishSpansContinueTheStoredTraceAndCarryNoPayload(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	stored := event("e1", "identity.events", "user-1")
	stored.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	unrelated := event("e2", "url.events", "url-1") // written outside a request: no stored context
	store := &fakeStore{claims: [][]ports.OutboxEvent{{stored, unrelated}}}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult {
		return []ports.PublishResult{{}, transient()}
	}}
	h := newHarness(t, store, pub, testConfig(), 2, WithTracerProvider(provider))

	h.run(t)

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	byName := map[string]tracetest.SpanStub{}
	for _, s := range spans {
		byName[s.Name] = s
	}

	continued := byName["identity.events publish"]
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", continued.SpanContext.TraceID().String(), "the publish continues the request's trace")
	assert.Equal(t, "00f067aa0ba902b7", continued.Parent.SpanID().String())
	assert.Equal(t, trace.SpanKindProducer, continued.SpanKind)
	assert.Equal(t, codes.Unset, continued.Status.Code)

	root := byName["url.events publish"]
	assert.False(t, root.Parent.IsValid(), "an event without a stored context starts its own trace")
	assert.Equal(t, codes.Error, root.Status.Code, "a failed record marks its span as an error")
	assert.Equal(t, "transient", root.Status.Description, "with the kind, not the error text")

	attrs := map[string]string{}
	for _, kv := range continued.Attributes {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	assert.Equal(t, map[string]string{
		"messaging.system": "kafka", "messaging.destination.name": "identity.events", "messaging.operation.type": "publish",
		"messaging.message.id": "e1", "outbox.event_type": "user.registered",
	}, attrs, "no payload, key or header values")

	// the message carries the publish span's context, so a consumer's span is its child
	require.Len(t, pub.calls, 1)
	assert.Contains(t, headerMap(pub.calls[0][0].Headers)["traceparent"], "-"+continued.SpanContext.SpanID().String()+"-")
}

func gaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			require.True(t, ok, "%s is %T", name, m.Data)
			require.Len(t, gauge.DataPoints, 1)
			return gauge.DataPoints[0].Value
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func counterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	want := attribute.NewSet(attrs...)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "%s is %T", name, m.Data)
			for _, point := range sum.DataPoints {
				if point.Attributes.Equals(&want) {
					return point.Value
				}
			}
		}
	}
	return 0
}

func TestRelay_ReportsItsBacklogLeadershipAndOutcomesAsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	clock := time.Now()
	store := &fakeStore{
		claims: [][]ports.OutboxEvent{{
			event("e1", "identity.events", "u1"), event("e2", "identity.events", "u2"),
			event("e3", "url.events", "x1"), event("bad", "url.events", "x2"),
		}},
		backlog: ports.Backlog{Unsent: 7, OldestUnsent: clock.Add(-90 * time.Second), Parked: 2},
	}
	pub := &fakePublisher{publish: func(int, []ports.Message) []ports.PublishResult {
		return []ports.PublishResult{{}, {}, transient(), permanent()}
	}}
	h := newHarness(t, store, pub, testConfig(), 2, WithMeterProvider(provider))
	h.clock = clock

	var during metricdata.ResourceMetrics
	baseSleep := h.relay.sleep
	h.relay.sleep = func(ctx context.Context, d time.Duration) {
		if len(h.sleeps) == 0 { // collect while this instance is the leader
			require.NoError(t, reader.Collect(context.Background(), &during))
		}
		baseSleep(ctx, d)
	}

	h.run(t)

	assert.Equal(t, 1.0, gaugeValue(t, during, "outbox.relay.leader"))
	assert.Equal(t, 7.0, gaugeValue(t, during, "outbox.relay.backlog"))
	assert.Equal(t, 2.0, gaugeValue(t, during, "outbox.relay.parked_rows"))
	assert.InDelta(t, 90.0, gaugeValue(t, during, "outbox.relay.oldest_unsent_age"), 11, "the age keeps growing between samples")

	var after metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &after))
	assert.Equal(t, 0.0, gaugeValue(t, after, "outbox.relay.leader"), "no longer the leader once the run ended")
	assert.Equal(t, 0.0, gaugeValue(t, after, "outbox.relay.backlog"), "and no stale backlog is reported")
	assert.Equal(t, int64(2), counterValue(t, after, "outbox.relay.published", attribute.String("destination", "identity.events")))
	assert.Equal(t, int64(0), counterValue(t, after, "outbox.relay.published", attribute.String("destination", "url.events")))
	assert.Equal(t, int64(1), counterValue(t, after, "outbox.relay.failures", attribute.String("kind", "transient")))
	assert.Equal(t, int64(1), counterValue(t, after, "outbox.relay.failures", attribute.String("kind", "permanent")))
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/internal/application/outbox`
Expected: build failure `undefined: NewRelay` (and `Config`, `Option`, `WithTracerProvider`, `WithMeterProvider`).

- [ ] **Step 3: Implement the metrics and the relay**

Create `app/internal/application/outbox/metrics.go`:

```go
package outbox

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/internal/application/outbox"

// metrics are the relay's instruments. The backlog numbers are sampled by the leader every few seconds and read by the
// gauges' callbacks; the oldest event's age is computed when it is read, so it keeps growing between samples.
type metrics struct {
	published metric.Int64Counter
	failures  metric.Int64Counter
	parked    metric.Int64Counter
	duration  metric.Float64Histogram

	leader       atomic.Bool
	unsent       atomic.Int64
	parkedRows   atomic.Int64
	oldestUnsent atomic.Int64 // unix nanoseconds; 0 when nothing waits
}

func newMetrics(provider metric.MeterProvider, now func() time.Time) (*metrics, error) {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	meter := provider.Meter(instrumentationName)
	m := &metrics{}

	var err error
	counter := func(name, description string) metric.Int64Counter {
		c, counterErr := meter.Int64Counter(name, metric.WithDescription(description))
		if counterErr != nil && err == nil {
			err = counterErr
		}
		return c
	}
	m.published = counter("outbox.relay.published", "Events acknowledged by the broker.")
	m.failures = counter("outbox.relay.failures", "Events the broker did not acknowledge, by kind (transient or permanent).")
	m.parked = counter("outbox.relay.parked", "Events the relay gave up on.")

	if m.duration, err = meter.Float64Histogram("outbox.relay.publish.duration",
		metric.WithUnit("s"), metric.WithDescription("Time to publish one batch.")); err != nil {
		return nil, err
	}

	gauge := func(name, unit, description string, read func() float64) {
		_, gaugeErr := meter.Float64ObservableGauge(name, metric.WithUnit(unit), metric.WithDescription(description),
			metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
				observer.Observe(read())
				return nil
			}))
		if gaugeErr != nil && err == nil {
			err = gaugeErr
		}
	}
	gauge("outbox.relay.leader", "{instance}", "1 while this instance is the active relay.", func() float64 {
		if m.leader.Load() {
			return 1
		}
		return 0
	})
	gauge("outbox.relay.backlog", "{event}", "Events waiting for delivery.", func() float64 { return float64(m.unsent.Load()) })
	gauge("outbox.relay.parked_rows", "{event}", "Events the relay gave up on.", func() float64 { return float64(m.parkedRows.Load()) })
	gauge("outbox.relay.oldest_unsent_age", "s", "Age of the oldest event waiting for delivery.", func() float64 {
		oldest := m.oldestUnsent.Load()
		if oldest == 0 {
			return 0
		}
		return now().Sub(time.Unix(0, oldest)).Seconds()
	})
	return m, err
}
```

Create `app/internal/application/outbox/relay.go`:

```go
// Package outbox is the transactional outbox relay (ADR-0011): one leader-elected worker that drains outbox_events into
// the broker with at-least-once delivery, keeps one key's events in order, parks events the broker permanently rejects
// and purges old sent rows.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// maxReasonLength bounds the error text stored on a row.
const maxReasonLength = 500

// Config tunes the relay. Every field must be set; NewConfig-style defaults live in the worker's configuration.
type Config struct {
	// PollInterval is the wait when a cycle finds nothing to deliver.
	PollInterval time.Duration
	// BatchSize is how many events per destination one cycle claims.
	BatchSize int
	// MaxAttempts is how many permanent rejections park an event.
	MaxAttempts int
	// StandbyInterval is how often an instance that is not the leader tries to become it.
	StandbyInterval time.Duration
	// Retention is how long sent events are kept; zero turns the purge off.
	Retention time.Duration
	// BacklogInterval is how often the leader samples the backlog for the gauges.
	BacklogInterval time.Duration
	// PurgeInterval is how often the leader purges; PurgeBatch is how many rows one purge statement deletes.
	PurgeInterval time.Duration
	PurgeBatch    int
	// BackoffMin and BackoffMax bound the wait after a cycle that delivered nothing; it doubles up to the maximum.
	BackoffMin time.Duration
	BackoffMax time.Duration
}

// Relay delivers the outbox to a broker.
type Relay struct {
	store     ports.OutboxDatastore
	publisher ports.EventPublisher
	cfg       Config

	tracer  trace.Tracer
	metrics *metrics

	// now and sleep are replaced by tests.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration)

	lastBeat atomic.Int64 // unix nanoseconds of the last completed cycle or standby attempt
}

// Option customises a Relay.
type Option func(*options)

type options struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// WithTracerProvider sets the tracer provider; the default is the global one.
func WithTracerProvider(provider trace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = provider }
}

// WithMeterProvider sets the meter provider; the default is the global one.
func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(o *options) { o.meterProvider = provider }
}

// NewRelay builds a relay. It returns an error only if the metric instruments cannot be created.
func NewRelay(store ports.OutboxDatastore, publisher ports.EventPublisher, cfg Config, opts ...Option) (*Relay, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.tracerProvider == nil {
		o.tracerProvider = otel.GetTracerProvider()
	}

	r := &Relay{
		store:     store,
		publisher: publisher,
		cfg:       cfg,
		tracer:    o.tracerProvider.Tracer(instrumentationName),
		now:       time.Now,
		sleep:     sleepContext,
	}
	var err error
	if r.metrics, err = newMetrics(o.meterProvider, func() time.Time { return r.now() }); err != nil {
		return nil, fmt.Errorf("create the outbox relay metrics: %w", err)
	}
	r.beat()
	return r, nil
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Healthy reports whether the relay has completed a cycle, or a standby attempt, within maxAge. A wedged loop is not.
func (r *Relay) Healthy(maxAge time.Duration) bool {
	return r.now().Sub(time.Unix(0, r.lastBeat.Load())) <= maxAge
}

func (r *Relay) beat() { r.lastBeat.Store(r.now().UnixNano()) }

// Run relays until ctx is cancelled and returns nil. An instance that is not the leader waits and retries; the leader
// delivers until it loses its lease or ctx ends. The batch in flight when ctx ends is finished first.
func (r *Relay) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		lease, err := r.store.Acquire(telemetry.Unsampled(ctx))
		if err != nil {
			if !errors.Is(err, ports.ErrNotLeader) {
				slog.WarnContext(ctx, "outbox relay: cannot take the lease", "error", err)
			}
			r.beat()
			r.sleep(ctx, r.cfg.StandbyInterval)
			continue
		}
		r.lead(ctx, lease)
		lease.Release()
		if ctx.Err() != nil {
			break
		}
		// Wait before asking again: a connection that keeps dying right after it is opened must not become a hot loop.
		r.sleep(ctx, r.cfg.StandbyInterval)
	}
	return nil
}

// lead delivers while it holds the lease.
func (r *Relay) lead(ctx context.Context, lease ports.Lease) {
	slog.InfoContext(ctx, "outbox relay: this instance is the leader")
	r.metrics.leader.Store(true)
	defer func() {
		// A standby has no view of the backlog: clear what it sampled so a stale number cannot outlive the leadership.
		r.metrics.leader.Store(false)
		r.metrics.unsent.Store(0)
		r.metrics.parkedRows.Store(0)
		r.metrics.oldestUnsent.Store(0)
		slog.InfoContext(ctx, "outbox relay: this instance is no longer the leader")
	}()

	var lastBacklog, lastPurge time.Time
	backoff := r.cfg.BackoffMin
	for ctx.Err() == nil {
		r.beat()
		housekeeping := telemetry.Unsampled(ctx)

		if err := lease.Alive(housekeeping); err != nil {
			slog.WarnContext(ctx, "outbox relay: lost the lease", "error", err)
			return
		}
		if now := r.now(); now.Sub(lastBacklog) >= r.cfg.BacklogInterval {
			r.sampleBacklog(housekeeping)
			lastBacklog = now
		}
		if now := r.now(); r.cfg.Retention > 0 && now.Sub(lastPurge) >= r.cfg.PurgeInterval {
			r.purge(housekeeping)
			lastPurge = now
		}

		events, err := r.store.Claim(housekeeping, r.cfg.BatchSize)
		if err != nil {
			slog.WarnContext(ctx, "outbox relay: cannot claim events", "error", err)
			r.sleep(ctx, backoff)
			backoff = r.nextBackoff(backoff)
			continue
		}
		if len(events) == 0 {
			r.sleep(ctx, r.cfg.PollInterval)
			continue
		}

		if r.deliver(ctx, events) {
			backoff = r.cfg.BackoffMin
			continue
		}
		r.sleep(ctx, backoff)
		backoff = r.nextBackoff(backoff)
	}
}

func (r *Relay) nextBackoff(current time.Duration) time.Duration {
	return min(current*2, r.cfg.BackoffMax)
}

// deliver publishes the claimed events and records the outcome of each. It reports whether at least one event was
// acknowledged and recorded as sent. The publish and the mark-as-sent run on a context that survives the shutdown
// signal, so a batch that is in flight when the process is told to stop is finished and recorded, not left to be
// published twice.
func (r *Relay) deliver(ctx context.Context, events []ports.OutboxEvent) bool {
	work := context.WithoutCancel(ctx)

	spans := make([]trace.Span, len(events))
	messages := make([]ports.Message, len(events))
	for i, event := range events {
		spanCtx, span := r.tracer.Start(storedTraceContext(work, event), event.Destination+" publish",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.String("messaging.destination.name", event.Destination),
				attribute.String("messaging.operation.type", "publish"),
				attribute.String("messaging.message.id", event.ID),
				attribute.String("outbox.event_type", event.EventType),
			))
		spans[i] = span
		messages[i] = buildMessage(spanCtx, event)
	}

	start := r.now()
	results := r.publisher.Publish(telemetry.Unsampled(work), messages)
	r.metrics.duration.Record(work, r.now().Sub(start).Seconds())

	if len(results) != len(events) {
		slog.ErrorContext(ctx, "outbox relay: the publisher returned the wrong number of results",
			"events", len(events), "results", len(results))
		results = make([]ports.PublishResult, len(events))
		for i := range results {
			results[i] = ports.PublishResult{Err: errors.New("no result from the publisher")}
		}
	}

	var sent []string
	for i, result := range results {
		event, span := events[i], spans[i]
		destination := attribute.String("destination", event.Destination)
		switch {
		case result.Err == nil:
			sent = append(sent, event.ID)
			r.metrics.published.Add(work, 1, metric.WithAttributes(destination))
		case result.Permanent:
			r.metrics.failures.Add(work, 1, metric.WithAttributes(attribute.String("kind", "permanent")))
			span.SetStatus(codes.Error, "permanent")
			r.reject(work, event, result.Err)
		default:
			r.metrics.failures.Add(work, 1, metric.WithAttributes(attribute.String("kind", "transient")))
			span.SetStatus(codes.Error, "transient")
			slog.WarnContext(ctx, "outbox relay: publish failed, will retry",
				"event_id", event.ID, "destination", event.Destination, "error", result.Err)
		}
		span.End()
	}

	if len(sent) == 0 {
		return false
	}
	if err := r.store.MarkSent(telemetry.Unsampled(work), sent); err != nil {
		slog.ErrorContext(ctx, "outbox relay: published events could not be marked as sent; they will be published again",
			"events", len(sent), "error", err)
		return false
	}
	return true
}

// reject counts a permanent rejection and parks the event once it has used up its attempts.
func (r *Relay) reject(ctx context.Context, event ports.OutboxEvent, cause error) {
	housekeeping := telemetry.Unsampled(ctx)
	reason := truncate(cause.Error(), maxReasonLength)

	attempts, err := r.store.RecordRejection(housekeeping, event.ID, reason)
	if err != nil {
		slog.ErrorContext(ctx, "outbox relay: cannot record a rejection", "event_id", event.ID, "error", err)
		return
	}
	slog.WarnContext(ctx, "outbox relay: the broker rejected an event",
		"event_id", event.ID, "destination", event.Destination, "attempt", attempts, "max_attempts", r.cfg.MaxAttempts, "error", cause)
	if attempts < r.cfg.MaxAttempts {
		return
	}
	if err := r.store.Park(housekeeping, event.ID, reason); err != nil {
		slog.ErrorContext(ctx, "outbox relay: cannot park an event", "event_id", event.ID, "error", err)
		return
	}
	r.metrics.parked.Add(ctx, 1)
	slog.ErrorContext(ctx, "outbox relay: parked an event the broker keeps rejecting; fix the cause, then re-queue it",
		"event_id", event.ID, "destination", event.Destination, "event_type", event.EventType, "reason", reason)
}

func (r *Relay) sampleBacklog(ctx context.Context) {
	backlog, err := r.store.Backlog(ctx)
	if err != nil {
		slog.WarnContext(ctx, "outbox relay: cannot read the backlog", "error", err)
		return
	}
	r.metrics.unsent.Store(backlog.Unsent)
	r.metrics.parkedRows.Store(backlog.Parked)
	if backlog.OldestUnsent.IsZero() {
		r.metrics.oldestUnsent.Store(0)
		return
	}
	r.metrics.oldestUnsent.Store(backlog.OldestUnsent.UnixNano())
}

// purge deletes sent events older than the retention, in batches, until a batch is not full.
func (r *Relay) purge(ctx context.Context) {
	cutoff := r.now().Add(-r.cfg.Retention)
	var total int
	for ctx.Err() == nil {
		deleted, err := r.store.Purge(ctx, cutoff, r.cfg.PurgeBatch)
		if err != nil {
			slog.WarnContext(ctx, "outbox relay: cannot purge sent events", "error", err)
			break
		}
		total += deleted
		if deleted < r.cfg.PurgeBatch {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "outbox relay: purged sent events", "deleted", total, "older_than", cutoff)
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/internal/application/outbox; go vet ./app/internal/application/outbox && go test -race -count=1 ./app/internal/application/outbox`
Expected: no gofmt output; vet clean; `ok` (the log lines the relay writes during the tests are normal output).

- [ ] **Step 5: Commit**

```bash
git add app/internal/application/outbox
git commit -m "feat(outbox): add the relay with leadership, parking, purge, spans and metrics"
```


---

### Task 5: The Postgres outbox adapter and its lease

**Files:**
- Create: `app/internal/adapters/postgres/outbox/doc.go`, `outbox_datastore_adapter.go`, `outbox_datastore_adapter_integration_test.go` (package `outboxdatastore`)

**Interfaces:**
- Consumes: the sqlc functions of Task 1, `ports.OutboxDatastore` and `ports.Lease` (Task 3), `postgres.StringToUUID`/`UUIDToString`/`ConnectionString`.
- Produces: `outboxdatastore.NewAdapter(client database.PostgresDatabaseClient, connString string, timeout time.Duration) (*Adapter, error)` implementing `ports.OutboxDatastore`. `Acquire` opens a dedicated connection (parsed through `pgxpool.ParseConfig`, tracer off) and takes `pg_try_advisory_lock` on a key derived from `"fupi.outbox.relay"`; the lease's `Alive` runs `SELECT 1` on it and `Release` closes it. Every statement is bounded by `timeout`. Tasks 11 and 12 construct it.

- [ ] **Step 1: Write the failing integration tests**

Create `app/internal/adapters/postgres/outbox/outbox_datastore_adapter_integration_test.go`. They run against a real Postgres with all migrations (claim order and per-destination limit, ties by id, skipping sent, parked and deleted rows, unreadable headers, empty key, mark sent once, invalid ids, rejection counting and parking and re-queueing, purge limits and what it never deletes, backlog, lease exclusivity and release, a lease whose backend is terminated, and the partial index serving the claim predicate):

```go
//go:build integration

package outboxdatastore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	outboxdatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/outbox"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	adapter *outboxdatastore.Adapter
	pool    *pgxpool.Pool
}

// newFixture starts Postgres with the migrations applied and builds an adapter on it.
func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)

	adapter, err := outboxdatastore.NewAdapter(client, client.GetDB().Config().ConnString(), 10*time.Second)
	require.NoError(t, err)
	return fixture{adapter: adapter, pool: client.GetDB()}
}

func newID() string { return entity.IDToString(entity.NewID()) }

var base = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

// insert writes one outbox row and returns its id; created is the row's created_at.
func (f fixture) insert(t *testing.T, destination, key string, created time.Time) string {
	t.Helper()
	id := newID()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload, created_at)
		VALUES ($1, NULLIF($2, ''), $3, 'user.registered', $4::json, $5::json, $6)`,
		id, key, destination, `{"event_id":"`+id+`","aggregate_id":"`+key+`"}`, `{"id":"`+id+`"}`, created)
	require.NoError(t, err)
	return id
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func ids(events []ports.OutboxEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func TestClaim_ReturnsTheOldestUnsentEventsOfEachDestinationInCreationOrder(t *testing.T) {
	f := newFixture(t)
	a1 := f.insert(t, "identity.events", "u1", base.Add(1*time.Second))
	b1 := f.insert(t, "url.events", "x1", base.Add(2*time.Second))
	a2 := f.insert(t, "identity.events", "u2", base.Add(3*time.Second))
	a3 := f.insert(t, "identity.events", "u3", base.Add(4*time.Second))
	b2 := f.insert(t, "url.events", "x2", base.Add(5*time.Second))

	events, err := f.adapter.Claim(context.Background(), 2)

	require.NoError(t, err)
	assert.Equal(t, []string{a1, b1, a2, b2}, ids(events), "two per destination, oldest first, in creation order overall (a3 is the third of its destination)")
	assert.NotContains(t, ids(events), a3)

	first := events[0]
	assert.Equal(t, "identity.events", first.Destination)
	assert.Equal(t, "u1", first.PartitionKey)
	assert.Equal(t, "user.registered", first.EventType)
	assert.JSONEq(t, `{"id":"`+a1+`"}`, string(first.Payload))
	assert.Equal(t, map[string]string{"event_id": a1, "aggregate_id": "u1"}, first.Headers)
	assert.Equal(t, 0, first.Attempts)
	assert.WithinDuration(t, base.Add(time.Second), first.CreatedAt, time.Millisecond)
}

func TestClaim_EventsWrittenAtTheSameInstantComeOutInTheOrderTheyWereRecorded(t *testing.T) {
	f := newFixture(t)
	same := base
	first := f.insert(t, "identity.events", "u1", same)
	second := f.insert(t, "identity.events", "u1", same)
	third := f.insert(t, "identity.events", "u1", same)

	events, err := f.adapter.Claim(context.Background(), 10)

	require.NoError(t, err)
	assert.Equal(t, []string{first, second, third}, ids(events), "ties break by id, which is a UUIDv7 generated in sequence")
}

func TestClaim_SkipsSentParkedAndSoftDeletedEvents(t *testing.T) {
	f := newFixture(t)
	waiting := f.insert(t, "identity.events", "u1", base)
	sent := f.insert(t, "identity.events", "u2", base.Add(time.Second))
	parked := f.insert(t, "identity.events", "u3", base.Add(2*time.Second))
	deleted := f.insert(t, "identity.events", "u4", base.Add(3*time.Second))
	f.exec(t, "UPDATE outbox_events SET sent_time = now() WHERE id = $1", sent)
	f.exec(t, "UPDATE outbox_events SET parked_at = now() WHERE id = $1", parked)
	f.exec(t, "UPDATE outbox_events SET deleted_at = now() WHERE id = $1", deleted)

	events, err := f.adapter.Claim(context.Background(), 10)

	require.NoError(t, err)
	assert.Equal(t, []string{waiting}, ids(events))
}

// A row whose headers are not an object must not stop every event behind it.
func TestClaim_ARowWithUnreadableHeadersIsStillClaimedWithoutThem(t *testing.T) {
	f := newFixture(t)
	id := f.insert(t, "identity.events", "u1", base)
	f.exec(t, `UPDATE outbox_events SET headers = '["not","an","object"]'::json WHERE id = $1`, id)

	events, err := f.adapter.Claim(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Empty(t, events[0].Headers)
}

func TestClaim_ARowWithoutAPartitionKeyHasAnEmptyKey(t *testing.T) {
	f := newFixture(t)
	f.insert(t, "identity.events", "", base)

	events, err := f.adapter.Claim(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Empty(t, events[0].PartitionKey)
}

func TestMarkSent_RecordsTheSendTimeOnlyOnceAndLeavesTheOthers(t *testing.T) {
	f := newFixture(t)
	one := f.insert(t, "identity.events", "u1", base)
	two := f.insert(t, "identity.events", "u2", base.Add(time.Second))
	three := f.insert(t, "identity.events", "u3", base.Add(2*time.Second))

	require.NoError(t, f.adapter.MarkSent(context.Background(), []string{one, three}))
	var firstSent time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT sent_time FROM outbox_events WHERE id = $1", one).Scan(&firstSent))
	require.NoError(t, f.adapter.MarkSent(context.Background(), []string{one}), "marking again is harmless")

	var again time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT sent_time FROM outbox_events WHERE id = $1", one).Scan(&again))
	assert.Equal(t, firstSent, again, "the original send time is kept")
	events, err := f.adapter.Claim(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, []string{two}, ids(events))
}

func TestMarkSent_AnInvalidIDIsAnErrorAndMarksNothing(t *testing.T) {
	f := newFixture(t)
	one := f.insert(t, "identity.events", "u1", base)

	err := f.adapter.MarkSent(context.Background(), []string{one, "not-a-uuid"})

	require.Error(t, err)
	events, claimErr := f.adapter.Claim(context.Background(), 10)
	require.NoError(t, claimErr)
	assert.Equal(t, []string{one}, ids(events))
}

func TestRecordRejectionCountsAndParkRemovesTheEventFromTheRelayUntilItIsRequeued(t *testing.T) {
	f := newFixture(t)
	id := f.insert(t, "identity.events", "u1", base)
	ctx := context.Background()

	first, err := f.adapter.RecordRejection(ctx, id, "message too large")
	require.NoError(t, err)
	second, err := f.adapter.RecordRejection(ctx, id, "message too large again")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, []int{first, second})

	require.NoError(t, f.adapter.Park(ctx, id, "message too large again"))
	var reason string
	var parked time.Time
	require.NoError(t, f.pool.QueryRow(ctx, "SELECT error_message, parked_at FROM outbox_events WHERE id = $1", id).Scan(&reason, &parked))
	assert.Equal(t, "message too large again", reason)
	assert.False(t, parked.IsZero())
	events, err := f.adapter.Claim(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, events, "a parked event is not claimed")

	// the documented way to re-queue it
	f.exec(t, "UPDATE outbox_events SET parked_at = NULL, attempts = 0 WHERE id = $1", id)
	events, err = f.adapter.Claim(ctx, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, 0, events[0].Attempts)
}

func TestPurge_DeletesOnlyOldSentEventsAndRespectsTheLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	oldSent := []string{f.insert(t, "identity.events", "u1", base), f.insert(t, "identity.events", "u2", base), f.insert(t, "identity.events", "u3", base)}
	recentSent := f.insert(t, "identity.events", "u4", base)
	unsentOld := f.insert(t, "identity.events", "u5", base.Add(-30*24*time.Hour))
	parkedOld := f.insert(t, "identity.events", "u6", base.Add(-30*24*time.Hour))
	for i, id := range oldSent {
		f.exec(t, "UPDATE outbox_events SET sent_time = $2 WHERE id = $1", id, base.Add(-10*24*time.Hour+time.Duration(i)*time.Minute))
	}
	f.exec(t, "UPDATE outbox_events SET sent_time = $2 WHERE id = $1", recentSent, base.Add(-time.Hour))
	f.exec(t, "UPDATE outbox_events SET parked_at = now() WHERE id = $1", parkedOld)
	cutoff := base.Add(-7 * 24 * time.Hour)

	deleted, err := f.adapter.Purge(ctx, cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted, "the limit bounds one purge")
	deleted, err = f.adapter.Purge(ctx, cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	deleted, err = f.adapter.Purge(ctx, cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)

	var remaining []string
	rows, err := f.pool.Query(ctx, "SELECT id::text FROM outbox_events ORDER BY created_at, id")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		remaining = append(remaining, id)
	}
	assert.ElementsMatch(t, []string{recentSent, unsentOld, parkedOld}, remaining, "recent sent, unsent and parked rows are never purged")
}

func TestBacklog_CountsWaitingAndParkedEventsAndFindsTheOldest(t *testing.T) {
	f := newFixture(t)
	empty, err := f.adapter.Backlog(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ports.Backlog{}, empty, "an empty outbox has no oldest event")

	f.insert(t, "identity.events", "u1", base.Add(time.Minute))
	f.insert(t, "identity.events", "u2", base)
	f.insert(t, "url.events", "x1", base.Add(2*time.Minute))
	sent := f.insert(t, "identity.events", "u3", base.Add(-time.Hour))
	parked := f.insert(t, "identity.events", "u4", base.Add(-2*time.Hour))
	f.exec(t, "UPDATE outbox_events SET sent_time = now() WHERE id = $1", sent)
	f.exec(t, "UPDATE outbox_events SET parked_at = now() WHERE id = $1", parked)

	backlog, err := f.adapter.Backlog(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), backlog.Unsent)
	assert.Equal(t, int64(1), backlog.Parked)
	assert.WithinDuration(t, base, backlog.OldestUnsent, time.Millisecond, "the oldest unsent, not the parked or the sent one")
}

func TestLease_OnlyOneHolderAtATimeAndItIsFreedWhenTheHolderGoes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.adapter.Acquire(ctx)
	require.NoError(t, err)
	require.NoError(t, first.Alive(ctx))

	_, err = f.adapter.Acquire(ctx)
	assert.ErrorIs(t, err, ports.ErrNotLeader, "a second instance does not get the lease")

	first.Release()
	second, err := f.adapter.Acquire(ctx)
	require.NoError(t, err, "released, the lease can be taken")
	second.Release()
}

// A crashed instance, or a failover to a new primary, drops the connection; the lease must go with it and the holder must notice.
func TestLease_IsLostWhenItsConnectionIsKilledAndTheHolderNoticesOnItsNextCheck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	held, err := f.adapter.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(held.Release)

	var terminated int
	require.NoError(t, f.pool.QueryRow(ctx, `
		SELECT count(pg_terminate_backend(pid)) FROM pg_locks
		WHERE locktype = 'advisory' AND granted AND pid <> pg_backend_pid()`).Scan(&terminated))
	require.Equal(t, 1, terminated, "exactly one backend holds the advisory lock")

	assert.Error(t, held.Alive(ctx), "the holder finds out the next time it checks")
	other, err := f.adapter.Acquire(ctx)
	require.NoError(t, err, "and another instance can take over")
	other.Release()
}

// The claim query depends on the partial index to stay cheap while the table holds a long history of sent rows.
func TestTheUnsentIndexServesTheClaimPredicate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)

	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM outbox_events
		WHERE sent_time IS NULL AND parked_at IS NULL AND deleted_at IS NULL ORDER BY destination, created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	assert.Contains(t, strings.Join(plan, "\n"), "ix_outbox_events_unsent_idx")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -tags integration -count=1 ./app/internal/adapters/postgres/outbox`
Expected: build failure: no non-test Go files, `undefined: outboxdatastore.NewAdapter`.

- [ ] **Step 3: Implement**

Create `app/internal/adapters/postgres/outbox/doc.go`:

```go
// Package outboxdatastore is the Postgres implementation of ports.OutboxDatastore: the queries the outbox relay runs
// and the advisory-lock lease that makes one relay instance the active one.
package outboxdatastore
```

Create `app/internal/adapters/postgres/outbox/outbox_datastore_adapter.go`:

```go
package outboxdatastore

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

// leaseKey is the advisory lock key of the relay lease, derived from a fixed name so every instance computes the same one.
var leaseKey = func() int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("fupi.outbox.relay"))
	return int64(h.Sum64())
}()

var _ ports.OutboxDatastore = (*Adapter)(nil)

// Adapter implements ports.OutboxDatastore on Postgres.
type Adapter struct {
	queries *postgresql.Queries
	// leaseConfig opens the dedicated connection the lease lives on. It is parsed through the pool's parser so the
	// pool_* parameters of the connection string are removed; a plain connection would send them to the server.
	leaseConfig *pgx.ConnConfig
	timeout     time.Duration
}

// NewAdapter builds the adapter. connString is the same connection string the pool was built from; timeout bounds every
// statement, because the relay runs some of them on a context that does not carry a deadline.
func NewAdapter(client database.PostgresDatabaseClient, connString string, timeout time.Duration) (*Adapter, error) {
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse the connection string for the outbox lease: %w", err)
	}
	// The lease connection is housekeeping and must not create a span per statement.
	poolConfig.ConnConfig.Tracer = nil

	return &Adapter{
		queries:     postgresql.New(client.GetDB()),
		leaseConfig: poolConfig.ConnConfig,
		timeout:     timeout,
	}, nil
}

func (a *Adapter) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.timeout)
}

// Acquire takes the relay lease: a session-level advisory lock on a dedicated connection. The lock disappears with
// the connection, so a crashed instance, or a failover to a new primary, releases it without anyone cleaning up.
func (a *Adapter) Acquire(ctx context.Context) (ports.Lease, error) {
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	conn, err := pgx.ConnectConfig(ctx, a.leaseConfig)
	if err != nil {
		return nil, fmt.Errorf("open the outbox lease connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leaseKey).Scan(&acquired); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("take the outbox lease: %w", err)
	}
	if !acquired {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, ports.ErrNotLeader
	}
	return &lease{conn: conn, timeout: a.timeout}, nil
}

type lease struct {
	conn    *pgx.Conn
	timeout time.Duration
}

// Alive runs a trivial statement on the lease's own connection; it fails once the connection, and with it the lock, is gone.
func (l *lease) Alive(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	var one int
	if err := l.conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("the outbox lease connection is not usable: %w", err)
	}
	return nil
}

// Release closes the connection, which releases the lock.
func (l *lease) Release() {
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	if err := l.conn.Close(ctx); err != nil {
		slog.Warn("closing the outbox lease connection", "error", err)
	}
}

// Claim returns up to perDestination of the oldest unsent, unparked events of every destination, in creation order.
func (a *Adapter) Claim(ctx context.Context, perDestination int) ([]ports.OutboxEvent, error) {
	ctx, cancel := a.bounded(ctx)
	defer cancel()

	rows, err := a.queries.QueryClaimOutboxEvents(ctx, int32(perDestination))
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
		LimitBy:   int32(limit),
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
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/internal/adapters/postgres/outbox; go vet -tags integration ./app/internal/adapters/postgres/outbox && go test -tags integration -count=1 -v ./app/internal/adapters/postgres/outbox 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: no gofmt output; thirteen `--- PASS` lines and `ok` (about 25 seconds; one Postgres container per test).

- [ ] **Step 5: Commit**

```bash
git add app/internal/adapters/postgres/outbox
git commit -m "feat(outbox): add the Postgres outbox datastore with an advisory-lock lease"
```

---

### Task 6: The franz-go producer (needs the go-ahead)

**Files:**
- Modify: `go.mod`, `go.sum`, `app/test/vars.go`
- Create: `app/pkg/infra/queue/kafka/producer.go`, `producer_test.go`, `producer_integration_test.go`
- Create: `app/test/test_kafka.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `kafka.Config{Brokers []string, ClientID string, PublishTimeout time.Duration}`, `kafka.Record{Topic string, Key, Value []byte, Headers []kafka.Header}`, `kafka.Header{Key, Value string}`, `kafka.NewProducer(cfg Config, extra ...kgo.Opt) (*Producer, error)`, `(*Producer).Produce(ctx, []Record) []error` (one error per record in order, bounded by `PublishTimeout`), `Ping`, `Close`, `kafka.IsPermanent(err) bool`; the test helper `test.StartKafka(t) *test.KafkaBroker` with `Brokers`, `Stop`, `Start`, `CreateTopic`, `Consume`. Tasks 7, 11 and 12 use them.

- [ ] **Step 1: STOP and ask for the go-ahead**

Use AskUserQuestion. Say, in the question text: from here the plan needs franz-go, which is already in the local module cache together with its dependencies (`pkg/kmsg` v1.14.0, `github.com/pierrec/lz4/v4` v4.1.30, `github.com/klauspost/compress` v1.20.0), so adding it downloads nothing (it is run with `GOPROXY=off`, which proves it). Two things the user must decide: (1) adding franz-go raises the existing indirect requirement `github.com/klauspost/compress` from v1.18.6 to v1.20.0, a compression library that is part of the production binary (a minor-version bump, from the cache); (2) the later `go mod tidy` and the Docker image builds of Tasks 13 and 16 use the network (`go mod tidy` may fetch test-only dependencies of the new modules; the builder runs `go mod download` for every module, from `proxy.golang.org`, verified against `sum.golang.org`, a few tens of MB, the same modules as the local build plus franz-go). No Docker image is pulled: `apache/kafka:4.3.1` and `postgres:16.2-alpine` are already local. Options: "Go ahead (Recommended)" and "Not now". If it is no, stop here and report Tasks 1 to 5 as done. Do not run any command below that touches the network until the answer is yes.

- [ ] **Step 2: Add franz-go from the cache**

Run:

```bash
GOFLAGS=-mod=mod GOPROXY=off go get github.com/twmb/franz-go/pkg/kgo@v1.22.1
git diff go.mod | grep '^[+-]' | grep -v '^+++\|^---'
git diff --stat go.sum
```

Expected: `go get` prints `upgraded github.com/klauspost/compress v1.18.6 => v1.20.0`, `upgraded github.com/pierrec/lz4/v4 v4.1.16 => v4.1.30`, `added github.com/twmb/franz-go v1.22.1` and `added github.com/twmb/franz-go/pkg/kmsg v1.14.0`; `go.mod` gains those four lines (the first two new requirements are marked `// indirect` until code imports them: step 11's `go mod tidy` fixes that) and `go.sum` gains eight lines. Nothing else. If the command fails with `module lookup disabled by GOPROXY=off`, a module is missing from the cache: stop and ask instead of retrying with the network on.

- [ ] **Step 3: Write the failing unit tests**

Create `app/pkg/infra/queue/kafka/producer_test.go`:

```go
package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIsPermanent(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                          {nil, false},
		"message too large":            {kerr.MessageTooLarge, true},
		"record list too large":        {kerr.RecordListTooLarge, true},
		"invalid topic":                {kerr.InvalidTopicException, true},
		"invalid record":               {kerr.InvalidRecord, true},
		"unsupported for the format":   {kerr.UnsupportedForMessageFormat, true},
		"topic authorization":          {kerr.TopicAuthorizationFailed, true},
		"cluster authorization":        {kerr.ClusterAuthorizationFailed, true},
		"wrapped permanent":            {fmt.Errorf("produce: %w", kerr.MessageTooLarge), true},
		"unknown topic may be created": {kerr.UnknownTopicOrPartition, false},
		"not leader":                   {kerr.NotLeaderForPartition, false},
		"request timed out":            {kerr.RequestTimedOut, false},
		"unknown server error":         {kerr.UnknownServerError, false},
		"record timeout":               {kgo.ErrRecordTimeout, false},
		"context deadline":             {context.DeadlineExceeded, false},
		"client closed":                {kgo.ErrClientClosed, false},
		"plain error":                  {errors.New("connection refused"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPermanent(tc.err))
		})
	}
}

func TestNewProducer_NeedsABrokerButNeverDialsOne(t *testing.T) {
	_, err := NewProducer(Config{ClientID: "test", PublishTimeout: time.Second})
	assert.Error(t, err)

	start := time.Now()
	producer, err := NewProducer(Config{Brokers: []string{"127.0.0.1:1"}, ClientID: "test", PublishTimeout: time.Second})
	require.NoError(t, err, "a broker that is down must not fail construction")
	t.Cleanup(producer.Close)
	assert.Less(t, time.Since(start), time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, producer.Ping(ctx), "Ping reports the broker as unreachable")
}

func TestProduce_FailsEveryRecordWithATransientErrorWhenNoBrokerAnswers(t *testing.T) {
	producer, err := NewProducer(Config{Brokers: []string{"127.0.0.1:1"}, ClientID: "test", PublishTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	start := time.Now()
	errs := producer.Produce(context.Background(), []Record{
		{Topic: "t", Key: []byte("k"), Value: []byte("1")},
		{Topic: "t", Key: []byte("k"), Value: []byte("2")},
	})

	require.Len(t, errs, 2)
	for i, err := range errs {
		require.Error(t, err, "record %d", i)
		assert.False(t, IsPermanent(err), "a broker that is down is not the record's fault: %v", err)
	}
	assert.Less(t, time.Since(start), 15*time.Second, "bounded by the delivery timeout")
}
```

- [ ] **Step 4: Run them to verify they fail**

Run: `go test ./app/pkg/infra/queue/kafka`
Expected: build failure `undefined: Config`, `undefined: NewProducer`, `undefined: IsPermanent` (and `Record`).

- [ ] **Step 5: Write the producer**

Create `app/pkg/infra/queue/kafka/producer.go`:

```go
package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Config configures a Producer.
type Config struct {
	// Brokers are the seed brokers, as host:port.
	Brokers []string
	// ClientID names this client to the brokers.
	ClientID string
	// PublishTimeout is how long a record may wait to be acknowledged before it fails with a timeout. Once one record of
	// a partition times out the records buffered after it for that partition fail too, which keeps the partition gapless.
	PublishTimeout time.Duration
}

// Header is a record header.
type Header struct {
	Key   string
	Value string
}

// Record is a message to produce.
type Record struct {
	Topic string
	// Key decides the partition: records with the same key go to the same partition in the order they are given. Nil
	// lets the client spread the record.
	Key     []byte
	Value   []byte
	Headers []Header
}

// Producer produces records to Kafka with an idempotent, all-replicas-acknowledged producer. It connects lazily:
// building one never fails because the brokers are down. Every Produce call is bounded by the publish timeout.
//
// By default franz-go refuses to fail a record whose request is already on its way, because with idempotent writes it
// cannot know whether the broker stored it; a broker that dies mid-request then keeps Produce waiting until it comes
// back, deadline or not. The outbox relay needs a bounded call and tolerates duplicates (it delivers at least once and
// consumers de-duplicate on the event ID), so cancellation of in-flight records is allowed.
type Producer struct {
	client  *kgo.Client
	timeout time.Duration
}

// NewProducer builds a Producer. extra options are applied last and are for tests.
func NewProducer(cfg Config, extra ...kgo.Opt) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: at least one broker is required")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		// Idempotent writes are the franz-go default and need all in-sync replicas to acknowledge; saying so keeps it explicit.
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordDeliveryTimeout(cfg.PublishTimeout),
		kgo.AllowIdempotentProduceCancellation(),
	}
	client, err := kgo.NewClient(append(opts, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("create the kafka client: %w", err)
	}
	return &Producer{client: client, timeout: cfg.PublishTimeout}, nil
}

// Produce produces the records and waits until each one is acknowledged or has failed, at most for the publish timeout. It returns one error per record,
// in the order of records; nil means the broker acknowledged it. Records with the same key reach their partition in the
// order given, and if one fails the ones after it for that partition fail too.
func (p *Producer) Produce(ctx context.Context, records []Record) []error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	pending := make([]*kgo.Record, len(records))
	index := make(map[*kgo.Record]int, len(records))
	for i, record := range records {
		headers := make([]kgo.RecordHeader, len(record.Headers))
		for j, header := range record.Headers {
			headers[j] = kgo.RecordHeader{Key: header.Key, Value: []byte(header.Value)}
		}
		pending[i] = &kgo.Record{Topic: record.Topic, Key: record.Key, Value: record.Value, Headers: headers}
		index[pending[i]] = i
	}

	// ProduceSync returns the results in the order the broker answered, not the order of the records, so each result is
	// matched to its record by the record it carries.
	errs := make([]error, len(records))
	for _, result := range p.client.ProduceSync(ctx, pending...) {
		errs[index[result.Record]] = result.Err
	}
	return errs
}

// Ping reports whether any broker answers.
func (p *Producer) Ping(ctx context.Context) error { return p.client.Ping(ctx) }

// Close closes the client.
func (p *Producer) Close() { p.client.Close() }

// permanentErrors are the broker answers that will never change for the same record: it is too large, its topic name is
// invalid, it is malformed, or the client is not allowed to write it. Everything else, including the broker being down, a
// timeout, an unknown topic (auto-creation is off, so it may be created later) and every retriable answer, is transient.
var permanentErrors = []*kerr.Error{
	kerr.MessageTooLarge,
	kerr.RecordListTooLarge,
	kerr.InvalidTopicException,
	kerr.InvalidRecord,
	kerr.UnsupportedForMessageFormat,
	kerr.TopicAuthorizationFailed,
	kerr.ClusterAuthorizationFailed,
}

// IsPermanent reports whether err says the broker will never accept the record as it is.
func IsPermanent(err error) bool {
	for _, permanent := range permanentErrors {
		if errors.Is(err, permanent) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: Run the unit tests**

Run: `gofmt -l app/pkg/infra/queue/kafka; go vet ./app/pkg/infra/queue/kafka && go test -race -count=1 ./app/pkg/infra/queue/kafka`
Expected: no gofmt output; `ok` (a producer for a broker that is down builds at once and its `Produce` fails every record with a transient error within the delivery timeout).

- [ ] **Step 7: Write the Kafka test broker**

Create `app/test/test_kafka.go`. It runs the local `apache/kafka:4.3.1` image like the stack's `kafka-single` (KRaft, topics not auto-created), on a free host port bound to the same port inside so the advertised address works, and can be stopped and started again on the same address:

```go
package test

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// KafkaBroker is a single-node Kafka (KRaft) running in a container, set up like the local stack's kafka-single: topics are
// not created automatically. It listens on a host port that stays the same when the broker is stopped and started again.
type KafkaBroker struct {
	// Brokers is the address to give a client.
	Brokers   []string
	container testcontainers.Container
}

// StartKafka starts a broker with the apache/kafka image the local stack uses and removes it when the test ends.
func StartKafka(t *testing.T) *KafkaBroker {
	t.Helper()
	ctx := context.Background()

	// The advertised address must be known before the broker starts, so a free host port is chosen first and bound to the same port inside.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port for kafka: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	hostPort := fmt.Sprintf("%d", port)

	req := testcontainers.ContainerRequest{
		Image:        TEST_KAFKA_IMAGE,
		ExposedPorts: []string{hostPort + "/tcp"},
		Env: map[string]string{
			"CLUSTER_ID":                                     "MkU3OEVBNTcwNTJENDM2Qk",
			"KAFKA_NODE_ID":                                  "1",
			"KAFKA_PROCESS_ROLES":                            "broker,controller",
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_LISTENERS":                                "INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:" + hostPort,
			"KAFKA_ADVERTISED_LISTENERS":                     "INTERNAL://localhost:9092,EXTERNAL://127.0.0.1:" + hostPort,
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "INTERNAL",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "false",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
			"KAFKA_NUM_PARTITIONS":                           "3",
			"KAFKA_DEFAULT_REPLICATION_FACTOR":               "1",
			"KAFKA_MIN_INSYNC_REPLICAS":                      "1",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_HEAP_OPTS":                                "-Xms256m -Xmx256m",
		},
		// The host port is the same as the one inside, so the advertised address works from the host.
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = network.PortMap{
				network.MustParsePort(hostPort + "/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: hostPort}},
			}
		},
		WaitingFor: wait.ForLog("Kafka Server started").WithStartupTimeout(containerReadyTimeout()),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("start kafka: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	return &KafkaBroker{Brokers: []string{"127.0.0.1:" + hostPort}, container: c}
}

// Stop stops the broker (a clean shutdown).
func (k *KafkaBroker) Stop(t *testing.T) {
	t.Helper()
	timeout := 30 * time.Second
	if err := k.container.Stop(context.Background(), &timeout); err != nil {
		t.Fatalf("stop kafka: %v", err)
	}
}

// Start starts the stopped broker again on the same address and waits until it answers.
func (k *KafkaBroker) Start(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := k.container.Start(ctx); err != nil {
		t.Fatalf("restart kafka: %v", err)
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(k.Brokers...))
	if err != nil {
		t.Fatalf("connect to kafka: %v", err)
	}
	defer client.Close()
	deadline := time.Now().Add(containerReadyTimeout())
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := client.Ping(pingCtx)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kafka did not answer after a restart: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// CreateTopic creates a topic with the given number of partitions.
func (k *KafkaBroker) CreateTopic(t *testing.T, name string, partitions int) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(k.Brokers...))
	if err != nil {
		t.Fatalf("connect to kafka: %v", err)
	}
	defer client.Close()

	topic := kmsg.NewCreateTopicsRequestTopic()
	topic.Topic = name
	topic.NumPartitions = int32(partitions)
	topic.ReplicationFactor = 1
	request := kmsg.NewPtrCreateTopicsRequest()
	request.Topics = append(request.Topics, topic)

	response, err := request.RequestWith(context.Background(), client)
	if err != nil {
		t.Fatalf("create topic %s: %v", name, err)
	}
	for _, result := range response.Topics {
		if result.ErrorCode != 0 {
			t.Fatalf("create topic %s: error code %d: %v", name, result.ErrorCode, result.ErrorMessage)
		}
	}
}

// Consume reads the topic from its start with a fresh client and returns the first n records, failing the test if they
// do not arrive within the timeout. Records are returned in the order of arrival at the client, which for one partition is
// the order in the log.
func (k *KafkaBroker) Consume(t *testing.T, topic string, n int, timeout time.Duration) []*kgo.Record {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(k.Brokers...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("connect a consumer: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var records []*kgo.Record
	for len(records) < n {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("read %d record(s) from %s: got %d before the timeout", n, topic, len(records))
		}
		fetches.EachRecord(func(r *kgo.Record) { records = append(records, r) })
	}
	return records
}
```

In `app/test/vars.go` add one line inside the `const (` block, directly after `TEST_POSTGRES_DATABASE_VERSION` (it is the image of the stack's Kafka, `deploy/kafka/compose.yml`; no comment line, which would make gofmt realign the whole block):

```go
	TEST_KAFKA_IMAGE = "apache/kafka:4.3.1"
```

then `gofmt -w app/test/vars.go`; `git diff app/test/vars.go` must show that one added line and nothing else.

- [ ] **Step 8: Write the integration tests**

Create `app/pkg/infra/queue/kafka/producer_integration_test.go` (keys, values and headers with per-key order and one partition per key; a record without a key; an oversized record that is permanent while the others in the batch arrive; a missing topic that is transient; and the outage in miniature: a batch fails as a whole while the broker is away, succeeds after it is back, and a reader sees every record once in order):

```go
//go:build integration

package kafka_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func newProducer(t *testing.T, broker *test.KafkaBroker, timeout time.Duration, extra ...kgo.Opt) *kafka.Producer {
	t.Helper()
	producer, err := kafka.NewProducer(kafka.Config{Brokers: broker.Brokers, ClientID: "integration-test", PublishTimeout: timeout}, extra...)
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	return producer
}

func headerValue(record *kgo.Record, key string) (string, bool) {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}

func TestProducer_DeliversKeysValuesAndHeadersAndKeepsEachKeysOrder(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	var records []kafka.Record
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("user-%d", i%3)
		records = append(records, kafka.Record{
			Topic: "identity.events", Key: []byte(key), Value: []byte(fmt.Sprintf(`{"key":"%s","n":%d}`, key, i)),
			Headers: []kafka.Header{{Key: "event_id", Value: fmt.Sprintf("e%d", i)}, {Key: "traceparent", Value: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}},
		})
	}

	errs := producer.Produce(context.Background(), records)
	for i, err := range errs {
		require.NoError(t, err, "record %d", i)
	}
	require.NoError(t, producer.Ping(context.Background()))

	consumed := broker.Consume(t, "identity.events", 12, 30*time.Second)
	partitionOf := map[string]int32{}
	nextExpected := map[string]int{"user-0": 0, "user-1": 1, "user-2": 2}
	for _, record := range consumed {
		key := string(record.Key)
		if known, ok := partitionOf[key]; ok {
			assert.Equal(t, known, record.Partition, "one key stays on one partition")
		}
		partitionOf[key] = record.Partition

		assert.Contains(t, string(record.Value), fmt.Sprintf(`"n":%d`, nextExpected[key]), "key %s arrived out of order", key)
		nextExpected[key] += 3

		eventID, ok := headerValue(record, "event_id")
		require.True(t, ok)
		assert.True(t, strings.HasPrefix(eventID, "e"))
		traceparent, ok := headerValue(record, "traceparent")
		require.True(t, ok)
		assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", traceparent)
	}
	assert.Len(t, partitionOf, 3)
}

func TestProducer_ARecordWithoutAKeyIsDelivered(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "url.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	errs := producer.Produce(context.Background(), []kafka.Record{{Topic: "url.events", Value: []byte(`{}`)}})

	require.NoError(t, errs[0])
	consumed := broker.Consume(t, "url.events", 1, 30*time.Second)
	assert.Nil(t, consumed[0].Key)
}

func TestProducer_AnOversizedRecordIsPermanentWhileTheOthersInTheBatchArrive(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	errs := producer.Produce(context.Background(), []kafka.Record{
		{Topic: "identity.events", Key: []byte("a"), Value: []byte("small-1")},
		{Topic: "identity.events", Key: []byte("b"), Value: make([]byte, 2_000_000)},
		{Topic: "identity.events", Key: []byte("c"), Value: []byte("small-2")},
	})

	require.Len(t, errs, 3)
	assert.NoError(t, errs[0])
	require.Error(t, errs[1])
	assert.True(t, kafka.IsPermanent(errs[1]), "too large will never succeed: %v", errs[1])
	assert.NoError(t, errs[2])
	assert.Len(t, broker.Consume(t, "identity.events", 2, 30*time.Second), 2)
}

// With topic auto-creation off, a missing topic may be created later, so it is transient: the relay waits, it does not park.
func TestProducer_AMissingTopicIsTransient(t *testing.T) {
	broker := test.StartKafka(t)
	producer := newProducer(t, broker, 3*time.Second, kgo.UnknownTopicRetries(1))

	start := time.Now()
	errs := producer.Produce(context.Background(), []kafka.Record{{Topic: "no.such.topic", Key: []byte("k"), Value: []byte("v")}})

	require.Error(t, errs[0])
	assert.False(t, kafka.IsPermanent(errs[0]), "%v", errs[0])
	assert.Less(t, time.Since(start), 30*time.Second)
}

// The outage drill in miniature: a batch fails as a whole while the broker is away (the partition stays gapless), the
// same batch succeeds after the broker is back, and a reader of the topic sees every record once, in order.
func TestProducer_ABatchThatFailsDuringAnOutageIsDeliveredInOrderAfterwards(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 3*time.Second)
	batch := func(from, to int) []kafka.Record {
		var records []kafka.Record
		for n := from; n <= to; n++ {
			records = append(records, kafka.Record{Topic: "identity.events", Key: []byte("user-1"), Value: []byte(fmt.Sprintf("%02d", n))})
		}
		return records
	}

	for i, err := range producer.Produce(context.Background(), batch(1, 5)) {
		require.NoError(t, err, "record %d", i+1)
	}

	broker.Stop(t)
	errs := producer.Produce(context.Background(), batch(6, 10))
	for i, err := range errs {
		require.Error(t, err, "record %d must fail while the broker is away", i+6)
		assert.False(t, kafka.IsPermanent(err), "%v", err)
	}
	assert.Error(t, producer.Ping(context.Background()), "the broker is down")

	broker.Start(t)
	for i, err := range producer.Produce(context.Background(), batch(6, 10)) {
		require.NoError(t, err, "record %d after the restart", i+6)
	}

	var seen []string
	for _, record := range broker.Consume(t, "identity.events", 10, 60*time.Second) {
		seen = append(seen, string(record.Value))
	}
	assert.Equal(t, []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10"}, seen, "every record once, in order")
}
```

- [ ] **Step 9: Run them**

Run: `gofmt -l app/test; go vet -tags integration ./app/test ./app/pkg/infra/queue/kafka && go test -tags integration -count=1 -v -timeout 10m ./app/pkg/infra/queue/kafka 2>&1 | grep -E '^(--- |ok|FAIL|panic)'`
Expected: `--- PASS` for all five `TestProducer_*` tests (the outage test takes about 15 seconds) and `ok`.

- [ ] **Step 10: Mutation check: the outage test must bite**

Temporarily delete the line `kgo.AllowIdempotentProduceCancellation(),` from `NewProducer` and run, with a short cap:

Run: `go test -tags integration -count=1 -timeout 2m -run TestProducer_ABatchThatFailsDuringAnOutage ./app/pkg/infra/queue/kafka`
Expected: `panic: test timed out after 2m0s` with the test blocked in `Produce` after the broker was stopped (franz-go waits for the broker to return, ignoring the delivery timeout). Restore the line and re-run the test: `ok`. If the test passes without the line, it proves nothing: stop and fix it before going on.

- [ ] **Step 11: Tidy the module graph**

This is covered by the go-ahead of step 1. Run:

```bash
go mod tidy
git diff go.mod | grep '^[+-]' | grep -v '^+++\|^---'
go build ./... && go vet ./app/... && go vet -tags integration ./app/pkg/infra/queue/kafka ./app/test
```

Expected: `franz-go` and `kmsg` listed as direct requirements, `github.com/moby/moby/api` moved to the direct block (the test broker imports its types), `klauspost/compress` at v1.20.0, no other version changed except by being dropped. If tidy changes another requirement's version, stop and ask.

- [ ] **Step 12: Commit**

```bash
git add go.mod go.sum app/test app/pkg/infra/queue/kafka
git commit -m "feat(kafka): add the franz-go producer and a Kafka test broker"
```

---

### Task 7: The Kafka event publisher

**Files:**
- Create: `app/internal/adapters/kafka/event_publisher.go`, `errors.go`, `event_publisher_test.go` (package `kafkaadapter`)

**Interfaces:**
- Consumes: `kafka.Producer`, `kafka.Record`, `kafka.IsPermanent` (Task 6), `ports.EventPublisher` (Task 3).
- Produces: `kafkaadapter.NewEventPublisher(p *kafka.Producer) *EventPublisher` implementing `ports.EventPublisher` (`Publish(ctx, []ports.Message) []ports.PublishResult`): messages map onto records field by field, each failure is classified on its own, a missing result is a transient failure. It also has `Ping(ctx) error` (the worker's readiness check) and `Close()`. Tasks 11 and 12 use it.

- [ ] **Step 1: Write the failing tests**

Create `app/internal/adapters/kafka/event_publisher_test.go`:

```go
package kafkaadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
)

type fakeProducer struct {
	produce func(records []kafka.Record) []error
	records []kafka.Record
	pinged  bool
	closed  bool
}

func (f *fakeProducer) Produce(_ context.Context, records []kafka.Record) []error {
	f.records = records
	return f.produce(records)
}
func (f *fakeProducer) Ping(context.Context) error { f.pinged = true; return nil }
func (f *fakeProducer) Close()                     { f.closed = true }

func TestPublish_MapsMessagesOntoRecords(t *testing.T) {
	fake := &fakeProducer{produce: func(r []kafka.Record) []error { return make([]error, len(r)) }}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), []ports.Message{{
		Topic: "identity.events", Key: []byte("user-1"), Value: []byte(`{"a":1}`),
		Headers: []ports.Header{{Key: "event_id", Value: "e1"}, {Key: "traceparent", Value: "00-aa-bb-01"}},
	}, {Topic: "url.events", Value: []byte(`{}`)}})

	require.Len(t, fake.records, 2)
	assert.Equal(t, kafka.Record{
		Topic: "identity.events", Key: []byte("user-1"), Value: []byte(`{"a":1}`),
		Headers: []kafka.Header{{Key: "event_id", Value: "e1"}, {Key: "traceparent", Value: "00-aa-bb-01"}},
	}, fake.records[0])
	assert.Nil(t, fake.records[1].Key)
	assert.Equal(t, []ports.PublishResult{{}, {}}, results)
}

func TestPublish_ClassifiesEachFailureOnItsOwn(t *testing.T) {
	transient := errors.New("broker not available")
	fake := &fakeProducer{produce: func([]kafka.Record) []error {
		return []error{nil, kerr.MessageTooLarge, transient, kerr.UnknownTopicOrPartition}
	}}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), make([]ports.Message, 4))

	require.Len(t, results, 4)
	assert.Equal(t, ports.PublishResult{}, results[0])
	assert.Equal(t, ports.PublishResult{Err: kerr.MessageTooLarge, Permanent: true}, results[1])
	assert.Equal(t, ports.PublishResult{Err: transient}, results[2])
	assert.False(t, results[3].Permanent, "an unknown topic may be created later")
	assert.Error(t, results[3].Err)
}

func TestPublish_AMissingResultIsATransientFailure(t *testing.T) {
	fake := &fakeProducer{produce: func([]kafka.Record) []error { return []error{nil} }}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), make([]ports.Message, 2))

	assert.NoError(t, results[0].Err)
	assert.Error(t, results[1].Err)
	assert.False(t, results[1].Permanent)
}

func TestPingAndCloseReachTheProducer(t *testing.T) {
	fake := &fakeProducer{}
	publisher := &EventPublisher{producer: fake}

	assert.NoError(t, publisher.Ping(context.Background()))
	publisher.Close()

	assert.True(t, fake.pinged)
	assert.True(t, fake.closed)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/internal/adapters/kafka`
Expected: build failure: no non-test Go files, `undefined: EventPublisher`.

- [ ] **Step 3: Implement**

Create `app/internal/adapters/kafka/errors.go`:

```go
package kafkaadapter

import "errors"

// errNoResult marks a message the producer returned no answer for; it is treated as a transient failure.
var errNoResult = errors.New("kafka: no result for the message")
```

Create `app/internal/adapters/kafka/event_publisher.go`:

```go
// Package kafkaadapter implements ports.EventPublisher on Kafka: it maps relay messages onto Kafka records and tells
// permanent failures from transient ones.
package kafkaadapter

import (
	"context"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
)

// producer is the part of kafka.Producer the adapter uses.
type producer interface {
	Produce(ctx context.Context, records []kafka.Record) []error
	Ping(ctx context.Context) error
	Close()
}

var _ ports.EventPublisher = (*EventPublisher)(nil)

// EventPublisher publishes outbox messages to Kafka.
type EventPublisher struct {
	producer producer
}

// NewEventPublisher wraps a Kafka producer.
func NewEventPublisher(p *kafka.Producer) *EventPublisher { return &EventPublisher{producer: p} }

// Publish produces the messages and returns one result per message, in order. A message the broker will never accept as it
// is (too large, an invalid topic name, ...) is a permanent failure; anything else that goes wrong is transient.
func (e *EventPublisher) Publish(ctx context.Context, messages []ports.Message) []ports.PublishResult {
	records := make([]kafka.Record, len(messages))
	for i, message := range messages {
		headers := make([]kafka.Header, len(message.Headers))
		for j, header := range message.Headers {
			headers[j] = kafka.Header{Key: header.Key, Value: header.Value}
		}
		records[i] = kafka.Record{Topic: message.Topic, Key: message.Key, Value: message.Value, Headers: headers}
	}

	errs := e.producer.Produce(ctx, records)
	results := make([]ports.PublishResult, len(messages))
	for i := range results {
		switch {
		case i >= len(errs):
			results[i] = ports.PublishResult{Err: errNoResult}
		case errs[i] != nil:
			results[i] = ports.PublishResult{Err: errs[i], Permanent: kafka.IsPermanent(errs[i])}
		}
	}
	return results
}

// Ping reports whether a broker answers.
func (e *EventPublisher) Ping(ctx context.Context) error { return e.producer.Ping(ctx) }

// Close closes the producer.
func (e *EventPublisher) Close() { e.producer.Close() }
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/internal/adapters/kafka; go vet ./app/internal/adapters/kafka && go test -race -count=1 ./app/internal/adapters/kafka`
Expected: no gofmt output; `ok`.

- [ ] **Step 5: Commit**

```bash
git add app/internal/adapters/kafka
git commit -m "feat(kafka): add the EventPublisher adapter"
```

---

### Task 8: A service-name default and a shared telemetry start

**Files:**
- Modify: `app/pkg/infra/telemetry/log.go`, `log_test.go`, `setup.go`, `setup_test.go`
- Create: `app/pkg/infra/telemetry/start.go`, `start_test.go`
- Modify: `app/cmd/main.go`
- Delete: `app/cmd/telemetry_test.go` (its tests move to `start_test.go`)

**Interfaces:**
- Consumes: `telemetry.Setup` (slice 4).
- Produces: `telemetry.DefaultServiceName = "fupi"`; `telemetry.ServiceName(fallback string) string`; `telemetry.Options.ServiceName`; `telemetry.Start(ctx, opts Options) (flush func())` (idempotent flush with its own 5 s deadline). Task 11's worker and the API's `main` use them.

- [ ] **Step 1: Write the failing tests**

In `app/pkg/infra/telemetry/log_test.go` replace the whole of `TestServiceName` (the last function) with:

```go
func TestServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	assert.Equal(t, "fupi", ServiceName(DefaultServiceName), "an empty value counts as unset")
	assert.Equal(t, "fupi-worker", ServiceName("fupi-worker"))

	t.Setenv("OTEL_SERVICE_NAME", "  fupi-api ")
	assert.Equal(t, "fupi-api", ServiceName("fupi-worker"), "the variable wins over the fallback")
}
```

In `app/pkg/infra/telemetry/setup_test.go` insert, directly above `func TestSetup_DisabledInstallsNothing`:

```go
// The worker passes its own service name; the API passes none and keeps "fupi". The variable still wins over both.
func TestSetup_TheApplicationsServiceNameIsTheDefaultAndTheEnvironmentStillWins(t *testing.T) {
	for name, tc := range map[string]struct {
		env, option, want string
	}{
		"no option, no variable": {"", "", "fupi"},
		"option only":            {"", "fupi-worker", "fupi-worker"},
		"variable beats option":  {"billing", "fupi-worker", "billing"},
	} {
		t.Run(name, func(t *testing.T) {
			c, endpoint := startCollector(t)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
			t.Setenv("OTEL_SERVICE_NAME", tc.env)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
			resetGlobals(t)

			shutdown, err := Setup(context.Background(), Options{ServiceName: tc.option})
			require.NoError(t, err)
			_, span := otel.Tracer("test").Start(context.Background(), "work")
			span.End()
			require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second))

			traces := c.traceRequests()
			require.Len(t, traces, 1)
			got, _ := resourceAttribute(traces[0].GetResourceSpans()[0].GetResource().GetAttributes(), "service.name")
			assert.Equal(t, tc.want, got)
		})
	}
}
```

Create `app/pkg/infra/telemetry/start_test.go` (these are the slice 4 tests of `startTelemetry`, moved here, plus the options pass-through):

```go
package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setupFunc = func(context.Context, Options) (func(context.Context) error, error)

func stubSetup(t *testing.T, stub setupFunc) {
	t.Helper()
	previous := setup
	setup = stub
	t.Cleanup(func() { setup = previous })
}

func TestStart_PassesTheOptionsOn(t *testing.T) {
	var got Options
	stubSetup(t, func(_ context.Context, opts Options) (func(context.Context) error, error) {
		got = opts
		return func(context.Context) error { return nil }, nil
	})

	Start(context.Background(), Options{ServiceName: "fupi-worker", ServiceVersion: "1.2.3", Environment: "staging"})()

	assert.Equal(t, Options{ServiceName: "fupi-worker", ServiceVersion: "1.2.3", Environment: "staging"}, got)
}

// The run context is cancelled by the SIGTERM that starts the shutdown, so a flush that reused it would give up at once.
func TestStart_TheFlushGetsItsOwnDeadlineAfterTheRunContextIsCancelled(t *testing.T) {
	var flushErr error
	var hasDeadline bool
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(ctx context.Context) error {
			flushErr = ctx.Err()
			_, hasDeadline = ctx.Deadline()
			return nil
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	flush := Start(ctx, Options{})
	cancel() // the SIGTERM

	flush()

	require.NoError(t, flushErr, "the flush context must not be the cancelled run context")
	assert.True(t, hasDeadline, "and it must be bounded")
}

// A telemetry problem must never stop the process.
func TestStart_ASetupFailureDisablesTelemetryInsteadOfStopping(t *testing.T) {
	buf := captureDefaultLogger(t)
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return nil, errors.New("bad exporter configuration")
	})

	flush := Start(context.Background(), Options{})

	require.NotNil(t, flush)
	assert.NotPanics(t, flush)
	assert.Contains(t, buf.String(), "telemetry is disabled")
	assert.Contains(t, buf.String(), "bad exporter configuration")
}

func TestStart_AFailingFlushIsLoggedNotFatal(t *testing.T) {
	buf := captureDefaultLogger(t)
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(context.Context) error { return errors.New("collector unreachable") }, nil
	})

	assert.NotPanics(t, Start(context.Background(), Options{}))

	assert.Contains(t, buf.String(), "flushing telemetry")
	assert.Contains(t, buf.String(), "collector unreachable")
}

// run flushes right after the server drains and again from a defer that covers its early returns.
func TestStart_TheFlushRunsOnlyOnce(t *testing.T) {
	var shutdowns int
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(context.Context) error {
			shutdowns++
			return nil
		}, nil
	})

	flush := Start(context.Background(), Options{})
	flush()
	flush()

	assert.Equal(t, 1, shutdowns)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/telemetry`
Expected: build failure `too many arguments in call to ServiceName`, `undefined: DefaultServiceName`, `unknown field ServiceName in struct literal of type Options`, `undefined: Start`, `undefined: setup`.

- [ ] **Step 3: Implement**

In `app/pkg/infra/telemetry/log.go` replace the `defaultServiceName` constant and `ServiceName` with:

```go
// DefaultServiceName is the service name of the API when OTEL_SERVICE_NAME is not set.
const DefaultServiceName = "fupi"

// ServiceName is the name this process reports: OTEL_SERVICE_NAME, or fallback when it is not set. The log lines and the
// telemetry resource both use it, so a log line and the trace it belongs to name the same service.
func ServiceName(fallback string) string {
	if name := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); name != "" {
		return name
	}
	return fallback
}
```

In `app/pkg/infra/telemetry/setup.go`: add `"cmp"` to the imports (before `"context"`), add the field to `Options` as its first field:

```go
	// ServiceName is the service name when OTEL_SERVICE_NAME is not set; it defaults to DefaultServiceName.
	ServiceName string
```

and in `newResource` change `attribute.String("service.name", defaultServiceName)` to `attribute.String("service.name", cmp.Or(opts.ServiceName, DefaultServiceName))`.

Create `app/pkg/infra/telemetry/start.go`:

```go
package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// flushTimeout bounds the final export of spans and metrics at shutdown.
const flushTimeout = 5 * time.Second

// setup is Setup. It is a variable so a test can replace it.
var setup = Setup

// Start installs the OpenTelemetry SDK and returns the function that flushes it. Call the flush once the server or the
// relay has stopped, so the last spans and the final metrics are exported; it flushes only once, however often it is
// called. Telemetry never stops the process: when the SDK cannot start the process runs without it, and a failing flush is
// only logged.
func Start(ctx context.Context, opts Options) (flush func()) {
	shutdown, err := setup(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "telemetry is disabled: the OpenTelemetry SDK could not start", "error", err)
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// ctx is already cancelled when this runs (that is what began the shutdown), so the flush gets its own deadline.
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
			defer cancel()
			if err := shutdown(flushCtx); err != nil {
				slog.WarnContext(flushCtx, "flushing telemetry", "error", err)
			}
		})
	}
}
```

In `app/cmd/main.go`:
- delete the constant `telemetryFlushTimeout` and its comment from the `const` block, the variable `setupTelemetry` with its comment, the whole function `startTelemetry` with its comment, and the import `"sync"`;
- change the logger line to `slog.SetDefault(telemetry.NewLogger(os.Stdout, cfg.Logging.Format, cfg.Logging.Level, telemetry.ServiceName(telemetry.DefaultServiceName)))`;
- in `run`, change `flushTelemetry := startTelemetry(ctx, cfg)` to `flushTelemetry := telemetry.Start(ctx, telemetry.Options{ServiceVersion: pkg.Version, Environment: cfg.Environment})`;
- run `gofmt -w app/cmd/main.go` (it removes the blank line left in the `const` block).

```bash
git rm app/cmd/telemetry_test.go
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/cmd app/pkg/infra/telemetry; go vet ./app/cmd ./app/pkg/infra/telemetry && go test -race -count=1 ./app/pkg/infra/telemetry ./app/cmd`
Expected: no gofmt output; both `ok`.

- [ ] **Step 5: Commit**

```bash
git add app/cmd app/pkg/infra/telemetry
git commit -m "refactor(telemetry): share the start and flush helper and let a process name its own service"
```

---

### Task 9: The worker's configuration

**Files:**
- Create: `app/config/worker.go`, `worker_test.go`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `config.LoadDatabase`, `LoadLogging`, the reader helpers (`splitList`, `validAddress`).
- Produces: `config.HealthSettings{Host string; Port int}`, `config.KafkaSettings`, `config.OutboxSettings`, `config.Worker`, and `LoadWorker`, `LoadWorkerHealth`, `LoadKafka`, `LoadOutbox` (all strict, every problem reported at once, values never echoed). Task 11 reads them.

- [ ] **Step 1: Write the failing tests**

Create `app/config/worker_test.go`:

```go
package config

import (
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadWorker_DefaultsMatchTheLocalStack(t *testing.T) {
	worker, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)

	assert.Equal(t, "development", worker.Environment)
	assert.Equal(t, HealthSettings{Host: "0.0.0.0", Port: 8086}, worker.Health)
	assert.Equal(t, 15*time.Second, worker.ShutdownTimeout)
	assert.Equal(t, KafkaSettings{Brokers: []string{"localhost:19092"}, ClientID: "fupi-worker", PublishTimeout: 10 * time.Second}, worker.Kafka)
	assert.Equal(t, OutboxSettings{
		PollInterval: 100 * time.Millisecond, BatchSize: 100, MaxAttempts: 3, StandbyInterval: 5 * time.Second, Retention: 7 * 24 * time.Hour,
	}, worker.Outbox)
	assert.Equal(t, "localhost", worker.Database.Postgres.Host, "the database settings are the API's")
	assert.Equal(t, "5432", worker.Database.Postgres.Port)
	assert.Equal(t, "json", worker.Logging.Format)
}

func TestLoadWorker_Overrides(t *testing.T) {
	worker, err := LoadWorker(lookupOf(map[string]string{
		"WORKER_HTTP_PORT": "9000", "SHUTDOWN_TIMEOUT": "30",
		"KAFKA_BROKERS": "kafka-1:9092, kafka-2:9092,kafka-3:9092", "KAFKA_CLIENT_ID": "relay-1", "KAFKA_PUBLISH_TIMEOUT": "20",
		"OUTBOX_POLL_INTERVAL_MS": "250", "OUTBOX_BATCH_SIZE": "500", "OUTBOX_MAX_ATTEMPTS": "5",
		"OUTBOX_STANDBY_INTERVAL": "2", "OUTBOX_RETENTION_DAYS": "30",
	}))
	require.NoError(t, err)

	assert.Equal(t, 9000, worker.Health.Port)
	assert.Equal(t, 30*time.Second, worker.ShutdownTimeout)
	assert.Equal(t, []string{"kafka-1:9092", "kafka-2:9092", "kafka-3:9092"}, worker.Kafka.Brokers)
	assert.Equal(t, "relay-1", worker.Kafka.ClientID)
	assert.Equal(t, 20*time.Second, worker.Kafka.PublishTimeout)
	assert.Equal(t, OutboxSettings{
		PollInterval: 250 * time.Millisecond, BatchSize: 500, MaxAttempts: 5, StandbyInterval: 2 * time.Second, Retention: 30 * 24 * time.Hour,
	}, worker.Outbox)
}

func TestLoadWorker_ARetentionOfZeroTurnsThePurgeOffAndAnEmptyValueCountsAsUnset(t *testing.T) {
	worker, err := LoadWorker(lookupOf(map[string]string{"OUTBOX_RETENTION_DAYS": "0", "KAFKA_BROKERS": "", "OUTBOX_BATCH_SIZE": ""}))
	require.NoError(t, err)

	assert.Equal(t, time.Duration(0), worker.Outbox.Retention)
	assert.Equal(t, []string{"localhost:19092"}, worker.Kafka.Brokers)
	assert.Equal(t, 100, worker.Outbox.BatchSize)
}

func TestLoadWorker_RejectsValuesOutOfRangeAndNamesTheVariablesNotTheValues(t *testing.T) {
	cases := map[string]map[string]string{
		"WORKER_HTTP_PORT":        {"WORKER_HTTP_PORT": "70000"},
		"KAFKA_BROKERS":           {"KAFKA_BROKERS": "no-port"},
		"KAFKA_PUBLISH_TIMEOUT":   {"KAFKA_PUBLISH_TIMEOUT": "0"},
		"OUTBOX_POLL_INTERVAL_MS": {"OUTBOX_POLL_INTERVAL_MS": "5"},
		"OUTBOX_BATCH_SIZE":       {"OUTBOX_BATCH_SIZE": "5000"},
		"OUTBOX_MAX_ATTEMPTS":     {"OUTBOX_MAX_ATTEMPTS": "0"},
		"OUTBOX_STANDBY_INTERVAL": {"OUTBOX_STANDBY_INTERVAL": "0"},
		"OUTBOX_RETENTION_DAYS":   {"OUTBOX_RETENTION_DAYS": "-1"},
		"SHUTDOWN_TIMEOUT":        {"SHUTDOWN_TIMEOUT": "0"},
	}
	for variable, env := range cases {
		t.Run(variable, func(t *testing.T) {
			_, err := LoadWorker(lookupOf(env))
			require.Error(t, err)
			assert.ErrorContains(t, err, variable)
		})
	}

	_, err := LoadWorker(lookupOf(map[string]string{"KAFKA_BROKERS": "secret-host-name-without-a-port"}))
	assert.NotContains(t, err.Error(), "secret-host-name", "an entry is named by position, never repeated")
	assert.ErrorContains(t, err, "entry 1")

	_, err = LoadWorker(lookupOf(map[string]string{"OUTBOX_BATCH_SIZE": "many"}))
	assert.ErrorContains(t, err, "OUTBOX_BATCH_SIZE must be an integer")
}

func TestLoadWorker_ReportsEveryProblemAtOnce(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"OUTBOX_BATCH_SIZE": "0", "KAFKA_BROKERS": "bad", "LOG_FORMAT": "xml", "DATABASE_PORT": "x"}))

	require.Error(t, err)
	for _, variable := range []string{"OUTBOX_BATCH_SIZE", "KAFKA_BROKERS", "LOG_FORMAT", "DATABASE_PORT"} {
		assert.ErrorContains(t, err, variable)
	}
}

func TestLoadWorker_RefusesTheDevelopmentDatabasePasswordOutsideDevelopmentAndTest(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production"}))
	assert.ErrorContains(t, err, "DATABASE_PASSWORD")

	for _, environment := range []string{"development", "test"} {
		_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": environment}))
		assert.NoError(t, err, environment)
	}
	_, err = LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password"}))
	assert.NoError(t, err)
}

func TestLoadWorker_NeedsNoAuthSecretAndNoRedis(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password"}))

	assert.NoError(t, err, "the worker reads neither AUTH_SECRET nor REDIS_*")
}

func TestLoadWorker_WarnsWhenEnvironmentIsUnsetAndTheDevelopmentPasswordIsInUse(t *testing.T) {
	worker, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)
	require.Len(t, worker.Warnings, 1)
	assert.Contains(t, worker.Warnings[0], "ENVIRONMENT is not set")

	explicit, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "development"}))
	require.NoError(t, err)
	assert.Empty(t, explicit.Warnings, "an explicit development environment is deliberate")
}

func TestLoadWorkerHealth_NeedsNothingElse(t *testing.T) {
	settings, err := LoadWorkerHealth(lookupOf(map[string]string{"SERVER_HOST": "127.0.0.1", "WORKER_HTTP_PORT": "9100", "KAFKA_BROKERS": "broken"}))

	require.NoError(t, err, "the health check must work without valid Kafka or database settings")
	assert.Equal(t, HealthSettings{Host: "127.0.0.1", Port: 9100}, settings)

	_, err = LoadWorkerHealth(lookupOf(map[string]string{"WORKER_HTTP_PORT": "abc"}))
	assert.ErrorContains(t, err, "WORKER_HTTP_PORT")
}

// .env.example is what a new developer copies to .env. If it drifts from the loader's defaults, the documented setup
// stops matching the stack.
func TestLoadWorker_EnvExampleDocumentsTheDefaults(t *testing.T) {
	example, err := godotenv.Read("../../.env.example")
	require.NoError(t, err)

	defaults, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)
	fromExample, err := LoadWorker(lookupOf(example))
	require.NoError(t, err)

	// An unset ENVIRONMENT warns and the example sets it, so the warnings differ on purpose.
	defaults.Warnings, fromExample.Warnings = nil, nil
	assert.Equal(t, defaults, fromExample)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/config`
Expected: build failure `undefined: LoadWorker`, `undefined: HealthSettings`, `undefined: KafkaSettings` and the other new names.

- [ ] **Step 3: Implement**

Create `app/config/worker.go`:

```go
package config

import (
	"errors"
	"time"
)

// KafkaSettings configures the Kafka producer.
type KafkaSettings struct {
	// Brokers are the seed brokers, as host:port.
	Brokers  []string
	ClientID string
	// PublishTimeout is how long one batch may take to be acknowledged.
	PublishTimeout time.Duration
}

// OutboxSettings configures the outbox relay.
type OutboxSettings struct {
	// PollInterval is the wait between cycles when nothing was claimed.
	PollInterval time.Duration
	// BatchSize is how many events per destination one cycle claims.
	BatchSize int
	// MaxAttempts is how many permanent rejections park an event.
	MaxAttempts int
	// StandbyInterval is how often a relay that is not the leader tries to become it.
	StandbyInterval time.Duration
	// Retention is how long sent events are kept; zero turns the purge off.
	Retention time.Duration
}

// HealthSettings is where the worker's health listener binds.
type HealthSettings struct {
	Host string
	Port int
}

// Worker is everything the worker process reads from its environment. It needs no AUTH_SECRET and no Redis.
type Worker struct {
	Environment     string
	Health          HealthSettings
	Database        DatabaseSettings
	Kafka           KafkaSettings
	Outbox          OutboxSettings
	Logging         LoggingSettings
	ShutdownTimeout time.Duration
	// Warnings are problems that do not stop startup but that the operator should see in the log.
	Warnings []string
}

// LoadWorker reads and validates the whole worker configuration. Every problem is reported, not just the first.
func LoadWorker(lookup Lookup) (Worker, error) {
	r := newReader(lookup)
	worker := Worker{
		Environment:     r.str("ENVIRONMENT", environmentDevelopment),
		ShutdownTimeout: r.units("SHUTDOWN_TIMEOUT", 15, time.Second),
	}
	if worker.ShutdownTimeout <= 0 {
		r.fail("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	errs := []error{r.err()}
	var err error
	worker.Health, err = LoadWorkerHealth(lookup)
	errs = append(errs, err)
	worker.Database, err = LoadDatabase(lookup)
	errs = append(errs, err)
	worker.Kafka, err = LoadKafka(lookup)
	errs = append(errs, err)
	worker.Outbox, err = LoadOutbox(lookup)
	errs = append(errs, err)
	worker.Logging, err = LoadLogging(lookup)
	errs = append(errs, err)

	if _, set := r.raw("ENVIRONMENT"); !set && worker.Database.Postgres.Url == "" && worker.Database.Postgres.Password == devDatabasePassword {
		worker.Warnings = append(worker.Warnings, "ENVIRONMENT is not set, so the development database password is accepted; set ENVIRONMENT (for example production) in a real deployment so it is refused")
	}
	return worker, errors.Join(errs...)
}

// LoadWorkerHealth reads where the health listener binds. The container health check uses only this loader, so it needs
// no database or Kafka settings.
func LoadWorkerHealth(lookup Lookup) (HealthSettings, error) {
	r := newReader(lookup)
	settings := HealthSettings{
		Host: r.str("SERVER_HOST", "0.0.0.0"),
		Port: r.integer("WORKER_HTTP_PORT", 8086),
	}
	if settings.Port < 1 || settings.Port > 65535 {
		r.fail("WORKER_HTTP_PORT must be a port between 1 and 65535")
	}
	return settings, r.err()
}

// LoadKafka reads the Kafka settings.
func LoadKafka(lookup Lookup) (KafkaSettings, error) {
	r := newReader(lookup)
	settings := KafkaSettings{
		Brokers:        splitList(r.str("KAFKA_BROKERS", "localhost:19092")),
		ClientID:       r.str("KAFKA_CLIENT_ID", "fupi-worker"),
		PublishTimeout: r.units("KAFKA_PUBLISH_TIMEOUT", 10, time.Second),
	}
	if len(settings.Brokers) == 0 {
		r.fail("KAFKA_BROKERS must list at least one host:port")
	}
	for i, entry := range settings.Brokers {
		if !validAddress(entry) {
			r.fail("KAFKA_BROKERS entry %d must be host:port with a port between 1 and 65535", i+1)
		}
	}
	if settings.PublishTimeout < time.Second {
		r.fail("KAFKA_PUBLISH_TIMEOUT must be at least 1 (seconds)")
	}
	return settings, r.err()
}

// LoadOutbox reads the outbox relay settings.
func LoadOutbox(lookup Lookup) (OutboxSettings, error) {
	r := newReader(lookup)
	settings := OutboxSettings{
		PollInterval:    r.units("OUTBOX_POLL_INTERVAL_MS", 100, time.Millisecond),
		BatchSize:       r.integer("OUTBOX_BATCH_SIZE", 100),
		MaxAttempts:     r.integer("OUTBOX_MAX_ATTEMPTS", 3),
		StandbyInterval: r.units("OUTBOX_STANDBY_INTERVAL", 5, time.Second),
		Retention:       r.units("OUTBOX_RETENTION_DAYS", 7, 24*time.Hour),
	}
	if settings.PollInterval < 10*time.Millisecond {
		r.fail("OUTBOX_POLL_INTERVAL_MS must be at least 10")
	}
	if settings.BatchSize < 1 || settings.BatchSize > 1000 {
		r.fail("OUTBOX_BATCH_SIZE must be between 1 and 1000")
	}
	if settings.MaxAttempts < 1 {
		r.fail("OUTBOX_MAX_ATTEMPTS must be at least 1")
	}
	if settings.StandbyInterval < time.Second {
		r.fail("OUTBOX_STANDBY_INTERVAL must be at least 1 (seconds)")
	}
	if settings.Retention < 0 {
		r.fail("OUTBOX_RETENTION_DAYS must not be negative (0 turns the purge off)")
	}
	return settings, r.err()
}
```

- [ ] **Step 4: Document the variables in `.env.example`**

In `.env.example` insert this block, followed by one blank line, directly above the line `# --- Local infrastructure (docker compose, ...` (the value allowlist of `scripts/infra_env_check.sh` starts at that line, and `localhost:19092` must stay above it):

```bash
# Outbox relay worker (app/cmd/worker, make infra.worker.up): delivers outbox_events to Kafka. From the host the single
# broker is localhost:19092; HA also has localhost:29092 and localhost:39092. Times are in the unit the name or the
# comment gives; OUTBOX_RETENTION_DAYS=0 turns the purge of sent events off.
KAFKA_BROKERS=localhost:19092
KAFKA_CLIENT_ID=fupi-worker
KAFKA_PUBLISH_TIMEOUT=10
OUTBOX_POLL_INTERVAL_MS=100
OUTBOX_BATCH_SIZE=100
OUTBOX_MAX_ATTEMPTS=3
OUTBOX_STANDBY_INTERVAL=5
OUTBOX_RETENTION_DAYS=7
WORKER_HTTP_PORT=8086
```

- [ ] **Step 5: Run the tests and the env check**

Run: `gofmt -l app/config/worker.go app/config/worker_test.go; go test -race -count=1 ./app/config && bash scripts/infra_env_check.sh && echo env-check-ok`
Expected: no gofmt output (`app/config/database.go` is old and not listed here); `ok`; `env-check-ok`. `TestLoadWorker_EnvExampleDocumentsTheDefaults` keeps the block pinned to the loader's defaults from now on.

- [ ] **Step 6: Commit**

```bash
git add app/config .env.example
git commit -m "feat(worker): add the worker configuration and document its variables"
```


---

### Task 10: The worker's health handler

**Files:**
- Create: `app/api/probes/http_handler.go`, `app/api/probes/http_handler_test.go`

**Interfaces:**
- Consumes: `health.Registry` (`Run`, `Report.Ready`, `SetDraining`), the existing `probes.LivePath` and `probes.ReadyPath`.
- Produces: `probes.NewHTTPHandler(registry *health.Registry, alive func() bool) http.Handler`: `GET /health` answers 200 `{"status":"ok"}` while `alive()` is true and 503 `{"status":"stalled"}` when it is not; `GET /health/ready` answers the registry's report with the status the API's router gives it (200, or 503 when a required check fails or the process is draining); any other path or method is not served. Task 11 mounts it.

- [ ] **Step 1: Write the failing tests**

Create `app/api/probes/http_handler_test.go` (liveness follows the loop and ignores the dependencies, readiness reports each dependency like the API does, draining is 503, and nothing but the two GET probes is served):

```go
package probes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpGet(t *testing.T, handler http.Handler, method, path string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	body, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	return recorder.Code, string(body)
}

func workerRegistry(postgres, kafka error) *health.Registry {
	registry := health.NewRegistry(time.Second)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: func(context.Context) error { return postgres }})
	registry.Add(health.Check{Name: "kafka", Required: true, Fn: func(context.Context) error { return kafka }})
	return registry
}

func TestHTTPHandler_LivenessFollowsTheLoopNotTheDependencies(t *testing.T) {
	registry := workerRegistry(errors.New("down"), errors.New("down"))
	registry.SetDraining()

	status, body := httpGet(t, NewHTTPHandler(registry, func() bool { return true }), "GET", LivePath)
	assert.Equal(t, 200, status, "a down dependency is no reason to restart the process")
	assert.JSONEq(t, `{"status":"ok"}`, body)

	status, body = httpGet(t, NewHTTPHandler(registry, func() bool { return false }), "GET", LivePath)
	assert.Equal(t, 503, status, "a stalled loop is")
	assert.JSONEq(t, `{"status":"stalled"}`, body)
}

func TestHTTPHandler_ReadinessReportsEachDependencyLikeTheAPI(t *testing.T) {
	cases := map[string]struct {
		registry   *health.Registry
		wantStatus int
		wantBody   string
	}{
		"all up":     {workerRegistry(nil, nil), 200, `{"status":"ok","checks":{"postgres":"up","kafka":"up"}}`},
		"kafka down": {workerRegistry(nil, errors.New("x")), 503, `{"status":"unavailable","checks":{"postgres":"up","kafka":"down"}}`},
		"both down":  {workerRegistry(errors.New("x"), errors.New("x")), 503, `{"status":"unavailable","checks":{"postgres":"down","kafka":"down"}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := httpGet(t, NewHTTPHandler(tc.registry, func() bool { return true }), "GET", ReadyPath)

			assert.Equal(t, tc.wantStatus, status)
			assert.JSONEq(t, tc.wantBody, body)
		})
	}
}

func TestHTTPHandler_ReadinessIsUnavailableWhileDraining(t *testing.T) {
	registry := workerRegistry(nil, nil)
	registry.SetDraining()

	status, body := httpGet(t, NewHTTPHandler(registry, func() bool { return true }), "GET", ReadyPath)

	assert.Equal(t, 503, status)
	assert.JSONEq(t, `{"status":"draining","checks":{}}`, body)
}

func TestHTTPHandler_ServesOnlyTheTwoProbesAndOnlyGet(t *testing.T) {
	handler := NewHTTPHandler(workerRegistry(nil, nil), func() bool { return true })

	status, _ := httpGet(t, handler, "GET", "/metrics")
	assert.Equal(t, 404, status)
	status, _ = httpGet(t, handler, "POST", LivePath)
	assert.Equal(t, 405, status)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/api/probes`
Expected: build failure `undefined: NewHTTPHandler`.

- [ ] **Step 3: Implement**

Create `app/api/probes/http_handler.go`:

```go
package probes

import (
	"encoding/json"
	"net/http"

	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
)

// NewHTTPHandler serves the same two probes as NewRouter over net/http, for processes that have no Fiber app (the outbox
// worker). Liveness answers 200 while alive returns true and 503 when it does not, which is how a wedged loop gets a
// container restarted; readiness answers the registry's report exactly as the API does.
func NewHTTPHandler(registry *health.Registry, alive func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+LivePath, func(w http.ResponseWriter, _ *http.Request) {
		if alive() {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "stalled"})
	})
	mux.HandleFunc("GET "+ReadyPath, func(w http.ResponseWriter, r *http.Request) {
		report := registry.Run(r.Context())
		status := http.StatusOK
		if !report.Ready() {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, report)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/api/probes; go vet ./app/api/probes && go test -race -count=1 ./app/api/probes`
Expected: no gofmt output; `ok` (the API's `router_test.go` tests still pass beside the new ones).

- [ ] **Step 5: Commit**

```bash
git add app/api/probes
git commit -m "feat(probes): add a net/http handler for the worker's liveness and readiness"
```

---

### Task 11: The worker command

**Files:**
- Create: `app/cmd/worker/main.go`, `app/cmd/worker/main_test.go`, `app/cmd/worker/main_integration_test.go`

**Interfaces:**
- Consumes: `config.LoadWorker`, `config.LoadWorkerHealth` (Task 9), `telemetry.Start`, `telemetry.ServiceName`, `telemetry.NewLogger` (Task 8), `probes.NewHTTPHandler` (Task 10), `outbox.NewRelay`, `outbox.Config`, `(*Relay).Run`, `(*Relay).Healthy` (Task 4), `outboxdatastore.NewAdapter` (Task 5), `kafka.NewProducer`, `kafkaadapter.NewEventPublisher`, `(*EventPublisher).Ping`/`Close` (Tasks 6 and 7), `postgres.NewPostgresClient`, `postgres.ConnectionString`.
- Produces: the `worker` binary: `worker` runs the relay, `worker healthcheck` probes `/health` on the configured port and exits 0 or 1. `run(ctx, cfg config.Worker) error` (the testable core), `relayConfig(config.OutboxSettings) outbox.Config` and `healthcheck(lookup, out) int`. The image, compose stack and live drill (Tasks 13 to 16) run it.

- [ ] **Step 1: Write the failing unit tests**

Create `app/cmd/worker/main_test.go` (run fails fast and clearly when Postgres is unreachable, the settings map onto the relay's configuration while the relay's own pacing stays fixed, and the healthcheck subcommand: alive, stalled, nothing listening, needing only the health settings, a wildcard bind and a bad port, and giving up after its timeout):

```go
package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/api/probes"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

// Postgres is the one required dependency: with it unreachable the process must exit with an error, not hang or relay.
func TestRun_ReturnsAnErrorWhenPostgresIsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about three seconds of connection retries")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true") // no collector to wait for at exit, and none of the developer's to feed
	cfg, err := config.LoadWorker(lookupOf(map[string]string{"DATABASE_PORT": "1", "DATABASE_CONN_TIMEOUT": "1"}))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = run(ctx, cfg)

	require.Error(t, err)
	assert.ErrorContains(t, err, "postgres")
}

func TestRelayConfig_MapsTheSettingsAndKeepsTheRelaysOwnPacing(t *testing.T) {
	cfg := relayConfig(config.OutboxSettings{
		PollInterval: 250 * time.Millisecond, BatchSize: 500, MaxAttempts: 5, StandbyInterval: 2 * time.Second, Retention: 48 * time.Hour,
	})

	assert.Equal(t, 250*time.Millisecond, cfg.PollInterval)
	assert.Equal(t, 500, cfg.BatchSize)
	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 2*time.Second, cfg.StandbyInterval)
	assert.Equal(t, 48*time.Hour, cfg.Retention)
	assert.Equal(t, 5*time.Second, cfg.BacklogInterval)
	assert.Equal(t, 10*time.Minute, cfg.PurgeInterval)
	assert.Equal(t, 1000, cfg.PurgeBatch)
	assert.Equal(t, 200*time.Millisecond, cfg.BackoffMin)
	assert.Equal(t, 10*time.Second, cfg.BackoffMax)
}

// healthServer answers GET /health with status and every other path with 404, like the worker's liveness route.
func healthServer(t *testing.T, status int) (host, port string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != probes.LivePath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	return host, port
}

func probeEnv(host, port string) config.Lookup {
	return lookupOf(map[string]string{"SERVER_HOST": host, "WORKER_HTTP_PORT": port})
}

func TestHealthcheck_ExitsZeroWhenTheWorkerIsAlive(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "ok")
}

func TestHealthcheck_ExitsOneWhenTheLoopIsStalled(t *testing.T) {
	host, port := healthServer(t, http.StatusServiceUnavailable)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "503")
}

func TestHealthcheck_ExitsOneWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	require.NoError(t, ln.Close())
	var out bytes.Buffer

	code := healthcheck(probeEnv("127.0.0.1", port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "healthcheck:")
}

func TestHealthcheck_NeedsOnlyTheHealthSettings(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	env := map[string]string{"SERVER_HOST": host, "WORKER_HTTP_PORT": port, "KAFKA_BROKERS": "broken", "OUTBOX_BATCH_SIZE": "0"}
	var out bytes.Buffer

	code := healthcheck(lookupOf(env), &out)

	assert.Equal(t, 0, code, "an unrelated bad setting must not make the container unhealthy: %s", out.String())
}

func TestHealthcheck_ReachesAWildcardBindThroughLoopbackAndRejectsABadPort(t *testing.T) {
	_, port := healthServer(t, http.StatusOK)
	for _, host := range []string{"0.0.0.0", "::", ""} {
		var out bytes.Buffer
		assert.Equal(t, 0, healthcheck(probeEnv(host, port), &out), "SERVER_HOST=%q: %s", host, out.String())
	}

	var out bytes.Buffer
	assert.Equal(t, 1, healthcheck(lookupOf(map[string]string{"WORKER_HTTP_PORT": "abc"}), &out))
	assert.Contains(t, out.String(), "WORKER_HTTP_PORT")
}

func TestHealthcheck_GivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	previous := healthcheckTimeout
	healthcheckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { healthcheckTimeout = previous })
	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	var out bytes.Buffer

	start := time.Now()
	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Less(t, time.Since(start), 2*time.Second)
}
```

Create `app/cmd/worker/main_integration_test.go` (the wiring against real Postgres and Kafka containers: a waiting event is delivered with its key and headers and recorded as sent, both probes answer with their expected bodies, and cancelling the run context, which stands for SIGTERM, returns without an error and frees the advisory lock):

```go
//go:build integration

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// The whole process against real Postgres and Kafka: it relays a waiting event, answers its probes, and on the stop signal
// finishes, releases the lease and returns without an error.
func TestRun_RelaysAWaitingEventAnswersItsProbesAndStopsCleanly(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OTEL_SDK_DISABLED", "true")

	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, databaseURL))
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)

	id := entity.IDToString(entity.NewID())
	_, err = pool.Exec(ctx, `INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload)
		VALUES ($1, 'user-1', 'identity.events', 'user.registered', $2::json, '{"hello":"worker"}'::json)`, id, `{"event_id":"`+id+`"}`)
	require.NoError(t, err)

	port := freePort(t)
	cfg, err := config.LoadWorker(lookupOf(map[string]string{
		"DATABASE_URL": databaseURL, "KAFKA_BROKERS": broker.Brokers[0], "WORKER_HTTP_PORT": port, "SERVER_HOST": "127.0.0.1",
		"OUTBOX_POLL_INTERVAL_MS": "20", "SHUTDOWN_TIMEOUT": "10",
	}))
	require.NoError(t, err)

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	finished := make(chan error, 1)
	go func() { finished <- run(runCtx, cfg) }()

	records := broker.Consume(t, "identity.events", 1, 60*time.Second)
	assert.Equal(t, "user-1", string(records[0].Key))
	assert.JSONEq(t, `{"hello":"worker"}`, string(records[0].Value))
	assert.Equal(t, id, headerOf(records[0].Headers, "event_id"))

	base := fmt.Sprintf("http://127.0.0.1:%s", port)
	status, body := httpGet(t, base+"/health")
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"status":"ok"}`, body)
	require.Eventually(t, func() bool {
		status, body = httpGet(t, base+"/health/ready")
		return status == 200
	}, 15*time.Second, 100*time.Millisecond)
	assert.JSONEq(t, `{"status":"ok","checks":{"postgres":"up","kafka":"up"}}`, body)

	require.Eventually(t, func() bool {
		var sent *time.Time
		_ = pool.QueryRow(ctx, "SELECT sent_time FROM outbox_events WHERE id = $1", id).Scan(&sent)
		return sent != nil
	}, 15*time.Second, 50*time.Millisecond, "the row is recorded as sent")

	stop() // the SIGTERM
	select {
	case err := <-finished:
		assert.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the worker did not stop")
	}
	var holders int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted").Scan(&holders))
	assert.Equal(t, 0, holders, "the lease was released")
}

func headerOf(headers []kgo.RecordHeader, key string) string {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run: `go test ./app/cmd/worker`
Expected: build failure `undefined: run`, `undefined: relayConfig`, `undefined: healthcheck`, `undefined: healthcheckTimeout`.

- [ ] **Step 3: Implement**

Create `app/cmd/worker/main.go`:

```go
// Command worker runs the Fupi background worker. Today that is the outbox relay: one leader-elected process that delivers
// the transactional outbox (ADR-0011) to Kafka with at-least-once delivery. The API never talks to Kafka (ADR-0015).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/sanctumlabs/fupi/app/api/probes"
	"github.com/sanctumlabs/fupi/app/config"
	kafkaadapter "github.com/sanctumlabs/fupi/app/internal/adapters/kafka"
	outboxdatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/outbox"
	"github.com/sanctumlabs/fupi/app/internal/application/outbox"
	"github.com/sanctumlabs/fupi/app/pkg"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
)

const (
	// serviceName is the worker's service name unless OTEL_SERVICE_NAME says otherwise; the API's is "fupi".
	serviceName = "fupi-worker"

	// livenessMaxAge is how long the relay loop may go without completing a cycle before liveness fails.
	livenessMaxAge = 30 * time.Second

	// The relay's own pacing; the settings in the environment are the ones an operator is expected to change.
	backlogInterval = 5 * time.Second
	purgeInterval   = 10 * time.Minute
	purgeBatch      = 1000
	backoffMin      = 200 * time.Millisecond
	backoffMax      = 10 * time.Second
)

// healthcheckTimeout bounds the container health probe. It is a variable so a test can shorten it.
var healthcheckTimeout = 2 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(os.LookupEnv, os.Stdout))
	}

	dotenvErr := godotenv.Load()

	cfg, err := config.LoadWorker(os.LookupEnv)
	// The logger is installed once the configuration is read, so LOG_LEVEL and LOG_FORMAT apply. On a configuration error
	// LoadWorker still returns the default logging settings, so the error itself is logged as JSON like everything else.
	slog.SetDefault(telemetry.NewLogger(os.Stdout, cfg.Logging.Format, cfg.Logging.Level, telemetry.ServiceName(serviceName)))
	if dotenvErr != nil {
		slog.Warn("no .env file found, relying on the environment", "error", dotenvErr)
	}
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings {
		slog.Warn(warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		// Hand signal handling back to the runtime as soon as the first signal arrives, so a second Ctrl-C ends a
		// stuck shutdown immediately instead of being swallowed.
		stop()
	}()

	err = run(ctx, cfg)
	stop()
	if err != nil {
		slog.Error("the worker stopped", "error", err)
		os.Exit(1)
	}
}

// healthcheck probes the worker running on this host and returns the process exit code: 0 when GET /health answers 200,
// 1 otherwise. It reads only the health settings, so the container's health check needs no database or Kafka settings and
// works in an image that has no shell, curl or wget.
func healthcheck(lookup config.Lookup, out io.Writer) int {
	settings, err := config.LoadWorkerHealth(lookup)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: invalid configuration: %v\n", err)
		return 1
	}

	target := "http://" + net.JoinHostPort(probeHost(settings.Host), strconv.Itoa(settings.Port)) + probes.LivePath
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(out, "healthcheck: %s answered %d\n", target, resp.StatusCode)
		return 1
	}
	_, _ = fmt.Fprintln(out, "healthcheck: ok")
	return 0
}

// probeHost turns a wildcard bind address, which cannot be dialed, into the loopback address that reaches it.
func probeHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

func relayConfig(settings config.OutboxSettings) outbox.Config {
	return outbox.Config{
		PollInterval:    settings.PollInterval,
		BatchSize:       settings.BatchSize,
		MaxAttempts:     settings.MaxAttempts,
		StandbyInterval: settings.StandbyInterval,
		Retention:       settings.Retention,
		BacklogInterval: backlogInterval,
		PurgeInterval:   purgeInterval,
		PurgeBatch:      purgeBatch,
		BackoffMin:      backoffMin,
		BackoffMax:      backoffMax,
	}
}

// run builds the relay from cfg and relays until ctx is cancelled, then finishes the batch in flight, flushes telemetry and
// closes its connections. Postgres is required: failing to reach it is an error. Kafka is not: the producer connects
// lazily, an unreachable Kafka only makes the relay back off, and readiness reports it as down.
func run(ctx context.Context, cfg config.Worker) error {
	// The deferred flush covers the early returns; the normal path flushes right after the relay stops (below).
	flushTelemetry := telemetry.Start(ctx, telemetry.Options{ServiceName: serviceName, ServiceVersion: pkg.Version, Environment: cfg.Environment})
	defer flushTelemetry()

	dbClient, err := postgres.NewPostgresClient(cfg.Database.Postgres)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer dbClient.Close()

	producer, err := kafka.NewProducer(kafka.Config{
		Brokers:        cfg.Kafka.Brokers,
		ClientID:       cfg.Kafka.ClientID,
		PublishTimeout: cfg.Kafka.PublishTimeout,
	})
	if err != nil {
		return fmt.Errorf("create the kafka producer: %w", err)
	}
	publisher := kafkaadapter.NewEventPublisher(producer)
	defer publisher.Close()

	store, err := outboxdatastore.NewAdapter(dbClient, postgres.ConnectionString(cfg.Database.Postgres), cfg.Database.OperationTimeout)
	if err != nil {
		return fmt.Errorf("create the outbox datastore: %w", err)
	}
	relay, err := outbox.NewRelay(store, publisher, relayConfig(cfg.Outbox))
	if err != nil {
		return fmt.Errorf("create the outbox relay: %w", err)
	}

	registry := health.NewRegistry(health.DefaultCheckTimeout)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: dbClient.HealthCheck})
	registry.Add(health.Check{Name: "kafka", Required: true, Fn: publisher.Ping})

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Health.Host, strconv.Itoa(cfg.Health.Port)))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Health.Port, err)
	}
	healthServer := &http.Server{
		Handler:           probes.NewHTTPHandler(registry, func() bool { return relay.Healthy(livenessMaxAge) }),
		ReadHeaderTimeout: 5 * time.Second,
	}
	healthErr := make(chan error, 1)
	go func() { healthErr <- healthServer.Serve(listener) }()
	slog.Info("worker health listening", "port", cfg.Health.Port)

	runCtx, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.Run(runCtx) }()

	var runErr error
	relayExited := false
	select {
	case <-ctx.Done():
	case err := <-healthErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("the health listener failed: %w", err)
		}
	case err := <-relayDone:
		relayExited = true
		runErr = fmt.Errorf("the relay stopped unexpectedly: %w", err)
	}

	// Shutdown: readiness turns to 503, then the relay finishes the batch it has in flight (bounded by the publish timeout).
	registry.SetDraining()
	slog.Info("shutting down the worker", "timeout", cfg.ShutdownTimeout.String())
	stopRelay()
	if !relayExited {
		select {
		case <-relayDone:
		case <-time.After(cfg.ShutdownTimeout):
			runErr = errors.Join(runErr, fmt.Errorf("the relay did not stop within %s", cfg.ShutdownTimeout))
		}
	}

	// The relay has stopped or given up: export the last spans and metrics now, before the connections close.
	flushTelemetry()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "closing the health listener", "error", err)
	}
	return runErr
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/cmd/worker; go vet ./app/cmd/worker && go vet -tags integration ./app/cmd/worker && go test -race -count=1 ./app/cmd/worker`
Expected: no gofmt output; `ok`.

Run: `go test -tags integration -count=1 -v -timeout 10m ./app/cmd/worker 2>&1 | grep -E '^(--- |ok|FAIL|panic)'`
Expected: `--- PASS: TestRun_RelaysAWaitingEventAnswersItsProbesAndStopsCleanly` and `ok` (Postgres and Kafka containers, about 30 seconds).

- [ ] **Step 5: Smoke the binary's refusal**

Run: `go build -o /tmp/fupi-worker-smoke ./app/cmd/worker && ENVIRONMENT=production /tmp/fupi-worker-smoke; echo "exit=$?"`

Expected: one JSON log line `"msg":"invalid configuration"` whose error names `DATABASE_PASSWORD` (the development password is refused in production) and `exit=1`. No Kafka or Postgres is contacted. Then remove the binary by its literal path: `rm /tmp/fupi-worker-smoke`.

- [ ] **Step 6: Commit**

```bash
git add app/cmd/worker
git commit -m "feat(worker): add the worker command that runs the outbox relay"
```

---

### Task 12: The relay end to end against Postgres and Kafka

**Files:**
- Create: `app/internal/application/outbox/relay_integration_test.go` (tag `integration`, package `outbox_test`)

**Interfaces:**
- Consumes: everything built so far (the real writer from Task 2, the Postgres adapter, the Kafka adapter and producer, the relay, the Postgres and Kafka test helpers).
- Produces: five end-to-end tests that pin the spec's success criteria 1 to 6 with real services; no production code.

- [ ] **Step 1: Write the tests**

Create `app/internal/application/outbox/relay_integration_test.go`. A written event arrives once with its key, headers and the trace of the request that wrote it (and the row is marked sent); a Kafka outage leaves the events waiting and they arrive in order when Kafka returns, with nothing parked; an event Kafka rejects permanently is parked after the configured attempts while the others still flow; of two relays one publishes and the other takes over when it stops; and the purge deletes old sent rows and nothing else:

```go
//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	kafkaadapter "github.com/sanctumlabs/fupi/app/internal/adapters/kafka"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	outboxdatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/outbox"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/application/outbox"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const topic = "identity.events"

// stack is Postgres with the migrations and a Kafka broker with the topic, plus everything to build relays against them.
type stack struct {
	client     database.PostgresDatabaseClient
	pool       *pgxpool.Pool
	connString string
	broker     *test.KafkaBroker
	store      *outboxdatastore.Adapter
}

func newStack(t *testing.T) stack {
	t.Helper()
	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)
	broker := test.StartKafka(t)
	broker.CreateTopic(t, topic, 3)

	connString := client.GetDB().Config().ConnString()
	store, err := outboxdatastore.NewAdapter(client, connString, 10*time.Second)
	require.NoError(t, err)
	return stack{client: client, pool: client.GetDB(), connString: connString, broker: broker, store: store}
}

// newStore builds another adapter on the same database, like a second worker would have.
func (s stack) newStore(t *testing.T) *outboxdatastore.Adapter {
	t.Helper()
	store, err := outboxdatastore.NewAdapter(s.client, s.connString, 10*time.Second)
	require.NoError(t, err)
	return store
}

func (s stack) publisher(t *testing.T, publishTimeout time.Duration) *kafkaadapter.EventPublisher {
	t.Helper()
	producer, err := kafka.NewProducer(kafka.Config{Brokers: s.broker.Brokers, ClientID: "relay-test", PublishTimeout: publishTimeout})
	require.NoError(t, err)
	publisher := kafkaadapter.NewEventPublisher(producer)
	t.Cleanup(publisher.Close)
	return publisher
}

func fastConfig() outbox.Config {
	return outbox.Config{
		PollInterval: 20 * time.Millisecond, BatchSize: 100, MaxAttempts: 3, StandbyInterval: 200 * time.Millisecond,
		BacklogInterval: 100 * time.Millisecond, PurgeInterval: time.Hour, PurgeBatch: 1000,
		BackoffMin: 50 * time.Millisecond, BackoffMax: 200 * time.Millisecond,
	}
}

// runRelay runs a relay until the returned stop function is called (also at the end of the test).
func runRelay(t *testing.T, store ports.OutboxDatastore, publisher ports.EventPublisher, cfg outbox.Config, opts ...outbox.Option) (stop func()) {
	t.Helper()
	relay, err := outbox.NewRelay(store, publisher, cfg, opts...)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = relay.Run(ctx)
	}()
	var once atomic.Bool
	stop = func() {
		if once.CompareAndSwap(false, true) {
			cancel()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop
}

type testEvent struct {
	EventID string `json:"id"`
	N       int    `json:"n"`
	Blob    string `json:"blob,omitempty"`
}

func (e testEvent) ID() string            { return e.EventID }
func (e testEvent) EventType() string     { return "user.registered" }
func (e testEvent) OccurredAt() time.Time { return time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) }

func (s stack) write(t *testing.T, ctx context.Context, aggregateID string, events ...testEvent) []string {
	t.Helper()
	domainEvents := make([]entity.DomainEvent, len(events))
	ids := make([]string, len(events))
	for i, e := range events {
		e.EventID = entity.IDToString(entity.NewID())
		domainEvents[i], ids[i] = e, e.EventID
	}
	require.NoError(t, postgresrepo.WriteOutboxEvents(ctx, postgresql.New(s.pool), topic, aggregateID, domainEvents))
	return ids
}

func (s stack) count(t *testing.T, where string) int {
	t.Helper()
	var n int
	require.NoError(t, s.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE "+where).Scan(&n))
	return n
}

func header(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func number(t *testing.T, record *kgo.Record) int {
	t.Helper()
	var payload testEvent
	require.NoError(t, json.Unmarshal(record.Value, &payload))
	return payload.N
}

func TestRelay_DeliversWrittenEventsWithTheirKeyHeadersAndTheTraceOfTheRequestThatWroteThem(t *testing.T) {
	s := newStack(t)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	// the request's span, as the HTTP middleware would have started it
	requestCtx, request := provider.Tracer("api").Start(context.Background(), "POST /auth/register")
	ids := s.write(t, requestCtx, "user-1", testEvent{N: 1}, testEvent{N: 2})
	ids = append(ids, s.write(t, requestCtx, "user-2", testEvent{N: 3})...)
	request.End()
	require.Equal(t, 3, s.count(t, "sent_time IS NULL"))

	runRelay(t, s.store, s.publisher(t, 10*time.Second), fastConfig(), outbox.WithTracerProvider(provider))

	records := s.broker.Consume(t, topic, 3, 60*time.Second)
	byID := map[string]*kgo.Record{}
	for _, r := range records {
		byID[header(r, "event_id")] = r
	}
	require.Len(t, byID, 3)

	first := byID[ids[0]]
	assert.Equal(t, "user-1", string(first.Key))
	assert.JSONEq(t, fmt.Sprintf(`{"id":"%s","n":1}`, ids[0]), string(first.Value))
	assert.Equal(t, "user.registered", header(first, "event_type"))
	assert.Equal(t, "user-1", header(first, "aggregate_id"))
	assert.Equal(t, "2026-10-05T10:00:00Z", header(first, "occurred_at"))
	assert.Equal(t, "application/json", header(first, "content-type"))

	// the trace runs request span -> outbox.publish span -> the record's traceparent
	var publish tracetest.SpanStub
	for _, span := range exporter.GetSpans() {
		if span.Name == topic+" publish" {
			publish = span
			break
		}
	}
	require.NotEmpty(t, publish.Name, "a publish span was recorded")
	assert.Equal(t, request.SpanContext().TraceID(), publish.SpanContext.TraceID(), "one trace from the request through the outbox")
	assert.Equal(t, request.SpanContext().SpanID(), publish.Parent.SpanID())
	traceparent := header(first, "traceparent")
	assert.Contains(t, traceparent, request.SpanContext().TraceID().String())

	// per key order, and the rows are recorded as sent
	var user1 []int
	for _, r := range records {
		if string(r.Key) == "user-1" {
			user1 = append(user1, number(t, r))
		}
	}
	assert.Equal(t, []int{1, 2}, user1)
	require.Eventually(t, func() bool { return s.count(t, "sent_time IS NULL") == 0 }, 15*time.Second, 50*time.Millisecond)
}

// The outage drill: Kafka away while events are written, then back. Nothing is lost, nothing is parked, and the order holds.
func TestRelay_DrainsTheBacklogInOrderOnceKafkaIsBack(t *testing.T) {
	s := newStack(t)
	s.broker.Stop(t)
	s.write(t, context.Background(), "user-1", testEvent{N: 1}, testEvent{N: 2}, testEvent{N: 3}, testEvent{N: 4}, testEvent{N: 5})
	runRelay(t, s.store, s.publisher(t, 2*time.Second), fastConfig())

	time.Sleep(5 * time.Second) // several failed cycles
	assert.Equal(t, 5, s.count(t, "sent_time IS NULL AND parked_at IS NULL"), "everything waits")
	assert.Equal(t, 0, s.count(t, "attempts > 0 OR parked_at IS NOT NULL"), "an outage is not a rejection: nothing is counted or parked")

	s.broker.Start(t)

	records := s.broker.Consume(t, topic, 5, 90*time.Second)
	var order []int
	for _, r := range records {
		order = append(order, number(t, r))
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, order, "the backlog arrives in order")
	require.Eventually(t, func() bool { return s.count(t, "sent_time IS NULL") == 0 }, 30*time.Second, 100*time.Millisecond)
}

func TestRelay_ParksAnEventKafkaRejectsAndKeepsDeliveringTheRest(t *testing.T) {
	s := newStack(t)
	good := s.write(t, context.Background(), "user-1", testEvent{N: 1}, testEvent{N: 3})
	poison := s.write(t, context.Background(), "user-2", testEvent{N: 2, Blob: strings.Repeat("x", 2_000_000)})
	runRelay(t, s.store, s.publisher(t, 10*time.Second), fastConfig())

	records := s.broker.Consume(t, topic, 2, 60*time.Second)
	assert.ElementsMatch(t, good, []string{header(records[0], "event_id"), header(records[1], "event_id")})

	require.Eventually(t, func() bool { return s.count(t, "parked_at IS NOT NULL") == 1 }, 30*time.Second, 50*time.Millisecond)
	var attempts int
	var reason string
	require.NoError(t, s.pool.QueryRow(context.Background(), "SELECT attempts, error_message FROM outbox_events WHERE id = $1", poison[0]).Scan(&attempts, &reason))
	assert.Equal(t, 3, attempts, "parked on the third permanent rejection")
	assert.Contains(t, strings.ToUpper(reason), "MESSAGE")
	assert.Equal(t, 0, s.count(t, "sent_time IS NULL AND parked_at IS NULL"), "the others were delivered and the poison event no longer blocks anything")

	backlog, err := s.store.Backlog(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), backlog.Parked)
}

// Two workers: one relays, the other waits; when the leader goes the other takes over and no event is delivered twice.
func TestRelay_OnlyOneOfTwoRelaysPublishesAndTheOtherTakesOverWhenItStops(t *testing.T) {
	s := newStack(t)
	pubA := &countingPublisher{EventPublisher: s.publisher(t, 10*time.Second)}
	pubB := &countingPublisher{EventPublisher: s.publisher(t, 10*time.Second)}
	stopA := runRelay(t, s.store, pubA, fastConfig())
	stopB := runRelay(t, s.newStore(t), pubB, fastConfig())

	s.write(t, context.Background(), "user-1", testEvent{N: 1}, testEvent{N: 2}, testEvent{N: 3})
	s.broker.Consume(t, topic, 3, 60*time.Second)
	require.Eventually(t, func() bool { return s.count(t, "sent_time IS NULL") == 0 }, 15*time.Second, 50*time.Millisecond)

	assert.Equal(t, int64(3), pubA.published.Load()+pubB.published.Load(), "three events, delivered once in total")
	assert.True(t, pubA.published.Load() == 0 || pubB.published.Load() == 0, "only one relay published: A=%d B=%d", pubA.published.Load(), pubB.published.Load())

	leader, follower, stopLeader := pubA, pubB, stopA
	if pubB.published.Load() > 0 {
		leader, follower, stopLeader = pubB, pubA, stopB
	}
	stopLeader()
	assert.Equal(t, int64(3), leader.published.Load())

	s.write(t, context.Background(), "user-1", testEvent{N: 4}, testEvent{N: 5}, testEvent{N: 6})
	records := s.broker.Consume(t, topic, 6, 60*time.Second)
	seen := map[string]bool{}
	for _, r := range records {
		seen[header(r, "event_id")] = true
	}
	assert.Len(t, seen, 6, "six different events: nothing was delivered twice")
	assert.Equal(t, int64(3), follower.published.Load(), "the follower took over and delivered the second batch")
}

func TestRelay_PurgesSentRowsOlderThanTheRetentionAndNothingElse(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	old := s.write(t, ctx, "user-1", testEvent{N: 1})[0]
	recent := s.write(t, ctx, "user-2", testEvent{N: 2})[0]
	unsent := s.write(t, ctx, "user-3", testEvent{N: 3})[0]
	_, err := s.pool.Exec(ctx, "UPDATE outbox_events SET sent_time = now() - interval '2 hours' WHERE id = $1", old)
	require.NoError(t, err)
	_, err = s.pool.Exec(ctx, "UPDATE outbox_events SET sent_time = now() WHERE id = $1", recent)
	require.NoError(t, err)
	_, err = s.pool.Exec(ctx, "UPDATE outbox_events SET parked_at = now() WHERE id = $1", unsent) // parked: neither relayed nor purged
	require.NoError(t, err)
	cfg := fastConfig()
	cfg.Retention, cfg.PurgeInterval = time.Hour, 100*time.Millisecond

	runRelay(t, s.store, s.publisher(t, 10*time.Second), cfg)

	require.Eventually(t, func() bool { return s.count(t, "id = '"+old+"'") == 0 }, 20*time.Second, 50*time.Millisecond, "the old sent row is purged")
	assert.Equal(t, 1, s.count(t, "id = '"+recent+"'"), "a recent sent row stays")
	assert.Equal(t, 1, s.count(t, "id = '"+unsent+"'"), "so does a row that was never sent")
}

type countingPublisher struct {
	ports.EventPublisher
	published atomic.Int64
}

func (p *countingPublisher) Publish(ctx context.Context, messages []ports.Message) []ports.PublishResult {
	results := p.EventPublisher.Publish(ctx, messages)
	for _, r := range results {
		if r.Err == nil {
			p.published.Add(1)
		}
	}
	return results
}
```

- [ ] **Step 2: Run them**

Run: `gofmt -l app/internal/application/outbox; go vet -tags integration ./app/internal/application/outbox && go test -tags integration -count=1 -v -timeout 15m ./app/internal/application/outbox 2>&1 | grep -E '^(--- |ok|FAIL|panic)'`
Expected: no gofmt output; five `--- PASS: TestRelay_*` lines (the outage and takeover tests take the longest, about a minute each) and `ok`. These tests pass on first run because Tasks 4 to 7 built everything they need: a failure here is a real defect in an earlier task, not a missing piece. Fix it in the owning task's file and add the unit or adapter test that would have caught it, test-first.

- [ ] **Step 3: Mutation check: the ordering and parking tests must bite**

Temporarily change the outer `ORDER BY ranked.created_at, ranked.id` of `QueryClaimOutboxEvents` (in the generated `app/internal/adapters/postgres/sql/outbox_relay_queries.sql.go`, only in the working tree; the first of the two occurrences, the SQL constant, not the comment) to `ORDER BY ranked.created_at DESC, ranked.id DESC` and run `go test -tags integration -count=1 -run 'TestRelay_DrainsTheBacklogInOrder' ./app/internal/application/outbox`. Expected: FAIL (the records arrive in reverse order). Restore it with `git checkout app/internal/adapters/postgres/sql/outbox_relay_queries.sql.go` and re-run: `ok`. If the test passes with the order reversed it proves nothing: stop and fix it.

- [ ] **Step 4: Commit**

```bash
git add app/internal/application/outbox/relay_integration_test.go
git commit -m "test(outbox): pin the relay's delivery, outage, parking, failover and purge against Postgres and Kafka"
```


---

### Task 13: The image, the compose stack and the make target

**Files:**
- Modify: `Dockerfile`, `docker-compose.yml`, `scripts/infra.sh`, `scripts/infra_test.sh`, `scripts/image_test.sh`, `.make/docker.mk`
- Create: `deploy/worker/compose.yml`

**Interfaces:**
- Consumes: the `app/cmd/worker` binary (Task 11).
- Produces: `/app/worker` in the image (same `fupi` image as the API, `fupi-app:local` in compose); the compose services `worker-ha` and `worker-single` (host port `127.0.0.1:8086`, health check `/app/worker healthcheck`); `scripts/infra.sh up|down|wait worker [ha|single]`; `make infra.worker.up|down [MODE=ha|single]`. Tasks 15 and 16 document and run them.

`make infra.*` targets run `create.envfile`, which only creates `.env` when it does not exist and never changes an existing one.

- [ ] **Step 1: Write the failing checks**

In `scripts/infra_test.sh` insert the three worker cases after the `wait resolves the app profile` case (the `up` case pins the whole command sequence: Postgres, then Kafka, then the worker, built):

```diff
--- a/scripts/infra_test.sh
+++ b/scripts/infra_test.sh
@@ -86,6 +86,26 @@
 expect_output "wait resolves the app profile" \
 "+ wait app-ha" \
   scripts/infra.sh wait app
+
+expect_output "up worker brings up postgres and kafka first, then builds and starts the worker" \
+"+ docker compose --profile postgres-ha rm -sf
++ docker compose --profile postgres-single up -d
++ wait postgres-single
++ docker compose --profile kafka-ha rm -sf
++ docker compose --profile kafka-single up -d
++ wait kafka-single
++ docker compose --profile worker-ha rm -sf
++ docker compose --profile worker-single up -d --build
++ wait worker-single" \
+  scripts/infra.sh up worker single
+
+expect_output "down worker stops only the worker" \
+"+ docker compose --profile worker-ha --profile worker-single rm -sf" \
+  scripts/infra.sh down worker
+
+expect_output "wait resolves the worker profile" \
+"+ wait worker-ha" \
+  scripts/infra.sh wait worker
 
 expect_exit "unknown stack is a usage error" 2 scripts/infra.sh up nope
 expect_exit "unknown mode is a usage error" 2 scripts/infra.sh up kafka triple
```

In `scripts/image_test.sh` add the worker file to the context check and two image checks (the worker is present and refuses the development password in production; its healthcheck runs read-only without capabilities and fails when nothing listens):

```diff
--- a/scripts/image_test.sh
+++ b/scripts/image_test.sh
@@ -25,7 +25,7 @@
 EOF
 )"
 
-  for want in /ctx/go.mod /ctx/go.sum /ctx/app/cmd/main.go /ctx/app/cmd/migrator/main.go \
+  for want in /ctx/go.mod /ctx/go.sum /ctx/app/cmd/main.go /ctx/app/cmd/migrator/main.go /ctx/app/cmd/worker/main.go \
     /ctx/app/internal/adapters/postgres/migrations/000001_initial_schema.up.sql; do
     if grep -qx "$want" <<<"$files"; then pass "context has ${want#/ctx/}"; else fail "context lacks ${want#/ctx/}"; fi
   done
@@ -85,6 +85,22 @@
     fail "the migrator is present and refuses the development password in production (exit $code: $out)"
   fi
 
+  out="$(docker run --rm --entrypoint /app/worker "$image" 2>&1)"
+  code=$?
+  if [ "$code" -eq 1 ] && grep -q 'DATABASE_PASSWORD' <<<"$out"; then
+    pass "the worker is present and refuses the development password in production"
+  else
+    fail "the worker is present and refuses the development password in production (exit $code: $out)"
+  fi
+
+  out="$(docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges:true --entrypoint /app/worker "$image" healthcheck 2>&1)"
+  code=$?
+  if [ "$code" -eq 1 ] && grep -q 'healthcheck:' <<<"$out"; then
+    pass "the worker's healthcheck runs read-only without capabilities and fails when nothing listens"
+  else
+    fail "the worker's healthcheck runs read-only without capabilities and fails when nothing listens (exit $code: $out)"
+  fi
+
   cid="$(docker create "$image")"
   value="$(docker cp "$cid:/app/migrations" - 2>/dev/null | tar -t 2>/dev/null | grep -c '\.up\.sql$')"
   docker rm "$cid" >/dev/null
```

- [ ] **Step 2: Run the infra checks to verify they fail**

Run: `bash scripts/infra_test.sh 2>&1 | grep -E '^(FAIL|all tests|[0-9]+ )' ; echo "exit=${PIPESTATUS[0]}"`
Expected: `FAIL` lines for the three worker cases (the stack name is unknown to `infra.sh`) and a non-zero exit. The image checks need an image and are run in step 7.

- [ ] **Step 3: Teach `scripts/infra.sh` and the Makefile the worker stack**

In `scripts/infra.sh` make these edits (the worker stack brings up Postgres and Kafka first, because the worker reads Postgres and writes to Kafka, and is built like the API):

```diff
--- a/scripts/infra.sh
+++ b/scripts/infra.sh
@@ -2,7 +2,7 @@
 # Starts, stops or waits on a local infrastructure stack in a given mode (see docs/LocalInfrastructure.md).
 #
 # Usage: scripts/infra.sh <up|down|wait> <stack> [ha|single]
-#   stacks: kafka redis postgres elk core full app (the mode applies) | observability legacy (no mode)
+#   stacks: kafka redis postgres elk core full app worker (the mode applies) | observability legacy (no mode)
 # Env:    COMPOSE       compose command (default "docker compose")
 #         WAIT_TIMEOUT  seconds to wait for services to become ready (default 300)
 #         DRY_RUN=1     print the commands instead of running them
@@ -35,7 +35,7 @@
 esac
 
 case "$stack" in
-  kafka | redis | postgres | elk | core | full | app)
+  kafka | redis | postgres | elk | core | full | app | worker)
     profile="$stack-$mode"
     other_profile="$stack-$other"
     both=(--profile "$stack-ha" --profile "$stack-single")
@@ -108,8 +108,13 @@
       "$(dirname "$0")/infra.sh" up postgres "$mode"
       "$(dirname "$0")/infra.sh" up redis "$mode"
     fi
+    if [ "$stack" = worker ]; then
+      # The worker reads Postgres (with its migrations) and writes to Kafka (with its topics).
+      "$(dirname "$0")/infra.sh" up postgres "$mode"
+      "$(dirname "$0")/infra.sh" up kafka "$mode"
+    fi
     [ -z "$other_profile" ] || run "${compose[@]}" --profile "$other_profile" rm -sf
-    if [ "$stack" = app ]; then up_args=(up -d --build); else up_args=(up -d); fi
+    if [ "$stack" = app ] || [ "$stack" = worker ]; then up_args=(up -d --build); else up_args=(up -d); fi
     run "${compose[@]}" --profile "$profile" "${up_args[@]}"
     wait_ready "$profile"
     ;;
```

In `.make/docker.mk` add the two profiles to `INFRA_PROFILES` and the two targets after `infra.app.down`:

```diff
--- a/.make/docker.mk
+++ b/.make/docker.mk
@@ -106,7 +106,7 @@
 MODE ?= ha
 INFRA := $(ROOT_DIR)/scripts/infra.sh
 COMPOSE := docker compose --project-directory $(ROOT_DIR)
-INFRA_PROFILES := kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single app-ha app-single
+INFRA_PROFILES := kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single app-ha app-single worker-ha worker-single
 
 # Everything except the legacy stack: `infra.clean` must not delete data that predates the infrastructure stacks
 INFRA_CLEAN_PROFILES := $(foreach p,$(filter-out legacy,$(INFRA_PROFILES)),--profile $(p))
@@ -170,6 +170,12 @@
 	@$(INFRA) up app $(MODE)
 infra.app.down: create.envfile ## Stop the API container (Postgres and Redis keep running)
 	@$(INFRA) down app
+
+.PHONY: infra.worker.up infra.worker.down
+infra.worker.up: create.envfile ## Build and start the outbox relay worker, with Postgres and Kafka brought up first (MODE=ha or single)
+	@$(INFRA) up worker $(MODE)
+infra.worker.down: create.envfile ## Stop the worker (Postgres and Kafka keep running)
+	@$(INFRA) down worker
 
 .PHONY: infra.config
 infra.config: create.envfile ## Check that every compose profile renders and the env defaults agree
```

- [ ] **Step 4: Run the infra checks**

Run: `bash scripts/infra_test.sh 2>&1 | tail -3`
Expected: ends with `all tests passed`.

- [ ] **Step 5: Add the compose stack**

Create `deploy/worker/compose.yml`:

```yaml
# The outbox relay worker (see docs/LocalInfrastructure.md). One service per mode; both run /app/worker from the repository image.
#   worker-ha     KAFKA_BROKERS lists the three brokers.
#   worker-single KAFKA_BROKERS is the single broker (it answers to the alias kafka-1).
# The services declare no depends_on: a dependency in a profile that is not enabled makes `docker compose config` fail, so
# scripts/infra.sh brings up and waits for Postgres and Kafka first, and restart: unless-stopped covers a manual start.
# The environment is an explicit list, not env_file: the worker must not see the other stacks' admin passwords.
x-worker: &worker
  build:
    context: ../..
    dockerfile: Dockerfile
  image: fupi-app:local
  # the image's own entrypoint is the API; the worker is another binary of the same image
  entrypoint: ["/app/worker"]
  restart: unless-stopped
  # longer than SHUTDOWN_TIMEOUT (15s) plus the 5s telemetry flush, so a stop never kills a batch in flight or the final export
  stop_grace_period: 25s
  read_only: true
  tmpfs: [/tmp]
  cap_drop: [ALL]
  security_opt: ["no-new-privileges:true"]
  ports: ["127.0.0.1:8086:8086"]
  # the image's HEALTHCHECK probes the API's port; the worker's liveness is its own subcommand
  healthcheck:
    test: ["CMD", "/app/worker", "healthcheck"]
    interval: 15s
    timeout: 5s
    retries: 5
    start_period: 20s
  networks:
    fupi:
      aliases: [fupi-worker]

x-worker-env: &worker-env
  ENVIRONMENT: development
  WORKER_HTTP_PORT: "8086"
  DATABASE_HOST: postgres
  DATABASE_PORT: "5432"
  DATABASE_NAME: ${PG_DATABASE:-fupidb}
  DATABASE_USERNAME: ${PG_APP_USER:-fupi-user}
  DATABASE_PASSWORD: ${PG_APP_PASSWORD:-fupi-pass}
  OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4317
  OTEL_SERVICE_NAME: fupi-worker

services:
  worker-ha:
    <<: *worker
    profiles: [worker-ha]
    environment:
      <<: *worker-env
      KAFKA_BROKERS: kafka-1:9092,kafka-2:9092,kafka-3:9092

  worker-single:
    <<: *worker
    profiles: [worker-single]
    environment:
      <<: *worker-env
      KAFKA_BROKERS: kafka-1:9092

networks:
  fupi:
    name: fupi
```

Include it from `docker-compose.yml`:

```diff
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@ -17,6 +17,8 @@
     env_file: .env
   - path: deploy/app/compose.yml
     env_file: .env
+  - path: deploy/worker/compose.yml
+    env_file: .env
 
 networks:
   fupi:
```

Run: `make infra.config 2>&1 | tail -4`
Expected: `ok: worker-ha`, `ok: worker-single` and `ok: all profiles` (every profile renders and the env defaults agree; no container is started).

- [ ] **Step 6: Build the worker into the image**

Edit `Dockerfile` (build the third binary, copy it, say so in the label):

```diff
--- a/Dockerfile
+++ b/Dockerfile
@@ -27,7 +27,8 @@
       -X github.com/sanctumlabs/fupi/app/pkg.GitCommit=${GIT_COMMIT} \
       -X github.com/sanctumlabs/fupi/app/pkg.BuildTime=${BUILD_TIME}"; \
     CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/fupi ./app/cmd; \
-    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/migrator ./app/cmd/migrator
+    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/migrator ./app/cmd/migrator; \
+    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/worker ./app/cmd/worker
 
 # Distribution: distroless static, no shell and no package manager, running as uid 65532.
 FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
@@ -37,7 +38,7 @@
 ARG BUILD_TIME=unknown
 
 LABEL org.opencontainers.image.title="fupi" \
-      org.opencontainers.image.description="Fupi URL shortener API and database migrator" \
+      org.opencontainers.image.description="Fupi URL shortener API, database migrator and outbox relay worker" \
       org.opencontainers.image.source="https://github.com/SanctumLabs/fupi" \
       org.opencontainers.image.licenses="MIT" \
       org.opencontainers.image.version="${VERSION}" \
@@ -46,7 +47,7 @@
 
 WORKDIR /app
 
-COPY --from=build /out/fupi /out/migrator /app/
+COPY --from=build /out/fupi /out/migrator /out/worker /app/
 COPY app/internal/adapters/postgres/migrations /app/migrations
 
 # A container started without ENVIRONMENT refuses the development secrets; compose overrides it for local use.
```

Run: `make lint.docker 2>&1 | tail -3`
Expected: ends with `Done linting Dockerfile` and prints no rule violation (it uses the pinned local hadolint image: nothing is pulled).

- [ ] **Step 7: Build the image and run the image checks**

This builds with the network (the go-ahead of Task 6 covers it: `go mod download` of the modules in `go.mod`, verified against the checksum database).

Run: `make build.docker DOCKER_IMAGE_TAG=fupi-worker-check:local 2>&1 | tail -3 && scripts/image_test.sh image fupi-worker-check:local 2>&1 | tail -20`
Expected: the build ends `Done building Docker image`; every line is `ok:` including `the worker is present and refuses the development password in production` and `the worker's healthcheck runs read-only without capabilities and fails when nothing listens`, and there is no `FAIL`.

Run: `scripts/image_test.sh context 2>&1 | tail -8`
Expected: `ok:   context has app/cmd/worker/main.go` and no `FAIL` (it needs the Go base image of the Dockerfile locally, which it is).

Remove the check image, by its literal name: `docker rmi fupi-worker-check:local`.

- [ ] **Step 8: Commit**

```bash
git add Dockerfile docker-compose.yml deploy/worker scripts .make/docker.mk
git commit -m "feat(worker): ship the worker in the image and add its compose stack and make targets"
```

---

### Task 14: Alert rules and the worker dashboard

**Files:**
- Modify: `deploy/observability/prometheus/rules/stack.yml`, `deploy/observability/prometheus/tests/stack_test.yml`
- Create: `deploy/observability/grafana/dashboards/fupi-worker.json`

**Interfaces:**
- Consumes: the relay's metrics (Task 4), predicted in Prometheus form: `outbox_relay_oldest_unsent_age_seconds`, `outbox_relay_parked_rows`, `outbox_relay_backlog`, `outbox_relay_leader`, `outbox_relay_published_total`, `outbox_relay_failures_total`, `outbox_relay_publish_duration_seconds_*`, labelled `service_name="fupi-worker"`.
- Produces: the alerts `OutboxBacklogOld` and `OutboxEventsParked`, and the dashboard `fupi-worker` (folder Fupi). Task 16 confirms the metric names on the running stack and corrects the queries here if they differ.

- [ ] **Step 1: Write the failing unit tests of the rules**

Append to `deploy/observability/prometheus/tests/stack_test.yml` (a backlog older than five minutes fires, a young one does not, no worker series raises neither alert, parked rows fire and zero parked does not):

```yaml
  - name: The oldest outbox event has been waiting for more than five minutes
    interval: 1m
    input_series:
      - {series: 'outbox_relay_oldest_unsent_age_seconds{service_name="fupi-worker"}', values: "301 400 500 600 700"}
    alert_rule_test:
      - eval_time: 4m
        alertname: OutboxBacklogOld
        exp_alerts:
          - exp_labels: {severity: warning}
            exp_annotations: {summary: The oldest outbox event has been waiting for delivery for more than 5 minutes}

  - name: A backlog that is only a minute old is not an alert
    interval: 1m
    input_series:
      - {series: 'outbox_relay_oldest_unsent_age_seconds{service_name="fupi-worker"}', values: "60 60 60 60 60"}
    alert_rule_test:
      - eval_time: 4m
        alertname: OutboxBacklogOld
        exp_alerts: []

  - name: No worker series at all, so neither outbox alert
    interval: 1m
    input_series:
      - {series: 'up{job="prometheus"}', values: "1 1 1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 6m
        alertname: OutboxBacklogOld
        exp_alerts: []
      - eval_time: 6m
        alertname: OutboxEventsParked
        exp_alerts: []

  - name: Parked outbox events
    interval: 1m
    input_series:
      - {series: 'outbox_relay_parked_rows{service_name="fupi-worker"}', values: "1 1 1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 6m
        alertname: OutboxEventsParked
        exp_alerts:
          - exp_labels: {severity: warning}
            exp_annotations: {summary: The outbox relay gave up on events; fix the cause and re-queue them (docs/LocalInfrastructure.md)}

  - name: Nothing parked, so no alert
    interval: 1m
    input_series:
      - {series: 'outbox_relay_parked_rows{service_name="fupi-worker"}', values: "0 0 0 0 0 0 0"}
    alert_rule_test:
      - eval_time: 6m
        alertname: OutboxEventsParked
        exp_alerts: []
```

- [ ] **Step 2: Run them to verify they fail**

Run: `docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 test rules /p/tests/stack_test.yml 2>&1 | grep -E 'FAILED|SUCCESS'`
Expected: `FAILED` (the two firing cases expect alerts that no rule defines yet). The image is the one the slice 1 plan already used and is local.

- [ ] **Step 3: Add the rules**

Append to the `rules:` list of `deploy/observability/prometheus/rules/stack.yml`:

```diff
--- a/deploy/observability/prometheus/rules/stack.yml
+++ b/deploy/observability/prometheus/rules/stack.yml
@@ -42,3 +42,21 @@
           severity: warning
         annotations:
           summary: p99 request latency is above 100ms
+
+      # Fed by the outbox relay worker's metrics (OpenTelemetry, via the collector). Both are silent when no worker runs:
+      # the series are absent, so a stack without the worker never raises them.
+      - alert: OutboxBacklogOld
+        expr: max(outbox_relay_oldest_unsent_age_seconds{service_name="fupi-worker"}) > 300
+        for: 2m
+        labels:
+          severity: warning
+        annotations:
+          summary: The oldest outbox event has been waiting for delivery for more than 5 minutes
+
+      - alert: OutboxEventsParked
+        expr: max(outbox_relay_parked_rows{service_name="fupi-worker"}) > 0
+        for: 5m
+        labels:
+          severity: warning
+        annotations:
+          summary: The outbox relay gave up on events; fix the cause and re-queue them (docs/LocalInfrastructure.md)
```

- [ ] **Step 4: Run the checks**

Run: `docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 check rules /p/rules/stack.yml 2>&1 | tail -2; docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 test rules /p/tests/stack_test.yml 2>&1 | tail -2`
Expected: `SUCCESS: 7 rules found`, then `SUCCESS`.

- [ ] **Step 5: Mutation check: the firing test must bite**

Temporarily change `> 300` to `> 3000` in the `OutboxBacklogOld` expression and run the `test rules` command again: Expected `FAILED` with `got:[]`. Restore `> 300` and re-run: `SUCCESS`.

- [ ] **Step 6: Add the dashboard**

Create `deploy/observability/grafana/dashboards/fupi-worker.json` (nine panels: active relays, waiting events, oldest waiting event, parked events, events published per second by destination, failures per second by kind, publish duration p50 and p99, backlog and its oldest age over time, and the leader per instance). Grafana's file provisioning picks it up from this folder, like the two existing dashboards:

```json
{
  "uid": "fupi-worker",
  "title": "Fupi worker",
  "tags": [
    "fupi"
  ],
  "schemaVersion": 39,
  "version": 1,
  "editable": false,
  "refresh": "30s",
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "templating": {
    "list": []
  },
  "annotations": {
    "list": []
  },
  "panels": [
    {"id":1,"type":"stat","title":"Active relays","gridPos":{"x":0,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum(outbox_relay_leader{service_name=\"fupi-worker\"})","legendFormat":""}]},
    {"id":2,"type":"stat","title":"Waiting for delivery","gridPos":{"x":6,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max(outbox_relay_backlog{service_name=\"fupi-worker\"})","legendFormat":""}]},
    {"id":3,"type":"stat","title":"Oldest waiting event","gridPos":{"x":12,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max(outbox_relay_oldest_unsent_age_seconds{service_name=\"fupi-worker\"})","legendFormat":""}],"fieldConfig":{"defaults":{"unit":"s"},"overrides":[]}},
    {"id":4,"type":"stat","title":"Parked events","gridPos":{"x":18,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max(outbox_relay_parked_rows{service_name=\"fupi-worker\"})","legendFormat":""}]},
    {"id":5,"type":"timeseries","title":"Events published per second","gridPos":{"x":0,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (destination) (rate(outbox_relay_published_total{service_name=\"fupi-worker\"}[5m]))","legendFormat":"{{destination}}"}],"fieldConfig":{"defaults":{"unit":"ops"},"overrides":[]}},
    {"id":6,"type":"timeseries","title":"Failed records per second","gridPos":{"x":12,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (kind) (rate(outbox_relay_failures_total{service_name=\"fupi-worker\"}[5m]))","legendFormat":"{{kind}}"}],"fieldConfig":{"defaults":{"unit":"ops"},"overrides":[]}},
    {"id":7,"type":"timeseries","title":"Batch publish time p99","gridPos":{"x":0,"y":12,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"histogram_quantile(0.99, sum by (le) (rate(outbox_relay_publish_duration_seconds_bucket{service_name=\"fupi-worker\"}[5m])))","legendFormat":"p99"}],"fieldConfig":{"defaults":{"unit":"s"},"overrides":[]}},
    {"id":8,"type":"logs","title":"Worker logs (Elasticsearch)","gridPos":{"x":0,"y":20,"w":24,"h":9},"datasource":{"type":"elasticsearch","uid":"elasticsearch"},"targets":[{"refId":"A","query":"service.name:fupi-worker","timeField":"@timestamp","metrics":[{"type":"logs","id":"1","settings":{"limit":"100"}}],"bucketAggs":[]}]},
    {"id":9,"type":"table","title":"Recent traces (Tempo)","gridPos":{"x":0,"y":29,"w":24,"h":9},"datasource":{"type":"tempo","uid":"tempo"},"targets":[{"refId":"A","queryType":"traceql","query":"{ resource.service.name = \"fupi-worker\" }","limit":20}]}
  ]
}
```

Run: `python3 -m json.tool deploy/observability/grafana/dashboards/fupi-worker.json > /dev/null && echo json-ok && python3 -c "import json;d=json.load(open('deploy/observability/grafana/dashboards/fupi-worker.json'));print(d['uid'],len(d['panels']),'panels')"`
Expected: `json-ok` then `fupi-worker 9 panels`.

- [ ] **Step 7: Commit**

```bash
git add deploy/observability
git commit -m "feat(observability): alert on an old or parked outbox backlog and add the worker dashboard"
```

---

### Task 15: Documentation, ADR-0018 and the ADR-0011 update

**Files:**
- Modify: `docs/LocalInfrastructure.md`, `docs/Deployment.md`, `docs/adr/0011-single-broker-agnostic-outbox.md`, `docs/adr/0017-the-api-exports-telemetry-over-otlp-and-keeps-logs-on-stdout.md`
- Create: `docs/adr/0018-the-outbox-relay-is-one-leader-elected-polling-worker-over-franz-go.md`

**Interfaces:**
- Consumes: the behaviour built in Tasks 1 to 14.
- Produces: the documentation the spec's success criterion 8 asks for. No code.

Everything written here must match the code: the variable names and defaults are those of `.env.example` (Task 9), the metric names are corrected in Task 16 if the running stack shows different ones (then correct this text too).

- [ ] **Step 1: Write ADR-0018**

Create `docs/adr/0018-the-outbox-relay-is-one-leader-elected-polling-worker-over-franz-go.md`:

````markdown
---
status: accepted
---

# The outbox relay is one leader-elected polling worker over franz-go, delivering at least once and parking events Kafka rejects for good

Domain events reach Kafka through a separate process, `worker` (`app/cmd/worker`, `/app/worker` in the image). The API never talks to Kafka (ADR-0015); it only writes `outbox_events` in the aggregate's transaction (ADR-0011).

**One active relay.** Workers compete for a Postgres advisory lock (`pg_try_advisory_lock`) held on a dedicated connection; the holder relays, the others stand by and retry every `OUTBOX_STANDBY_INTERVAL` seconds, and the lock is freed when the holder's connection goes away, so a crashed leader is replaced without any coordination service. One relay publishing in `(created_at, id)` order is what keeps one aggregate's events in order, and the outbox's volume (registrations and URL creations, not redirects) does not need parallel relays.

**Polling.** Every `OUTBOX_POLL_INTERVAL_MS` (default 100) the relay claims up to `OUTBOX_BATCH_SIZE` unsent, unparked rows per destination, publishes them in one call and marks the acknowledged ones sent. Per destination, so a destination whose topic is missing cannot starve the others. There is no `LISTEN/NOTIFY` and no `FOR UPDATE`: the lock already makes the relay the only reader, and the partial index on unsent rows makes the poll cheap.

**At least once.** The relay can stop between a publish and the update of `sent_time`, and a producer that gives up on a record in flight cannot know whether the broker stored it, so an event can be delivered twice. Consumers de-duplicate on the `event_id` header (the outbox row's UUIDv7). Exactly-once would need Kafka transactions spanning two systems.

**Failures are two different things.** A transient failure (Kafka down, a timeout, an unknown topic, anything not on the allowlist) never counts against an event and never parks one: the rows stay, the relay backs off from 200 ms to 10 s and the backlog alert says so. A permanent rejection, an explicit allowlist of broker answers that never change for the same record (`MESSAGE_TOO_LARGE`, `RECORD_LIST_TOO_LARGE`, `INVALID_TOPIC_EXCEPTION`, `INVALID_RECORD`, `UNSUPPORTED_FOR_MESSAGE_FORMAT`, `TOPIC_AUTHORIZATION_FAILED`, `CLUSTER_AUTHORIZATION_FAILED`), increments `attempts`; at `OUTBOX_MAX_ATTEMPTS` (default 3) the row is parked (`parked_at`, `error_message`) and the relay carries on. A parked row stays in the table until someone fixes the cause and clears `parked_at` and `attempts`; there is no dead-letter topic.

**The client is franz-go.** It is pure Go, so it builds with `CGO_ENABLED=0` into the distroless image (a librdkafka client would need cgo), it has an idempotent producer with per-record results, and it keeps a partition's records in order when one fails. Every `Produce` call is bounded by `KAFKA_PUBLISH_TIMEOUT` and the client allows cancelling in-flight records (`AllowIdempotentProduceCancellation`): by default franz-go will not fail a record whose request is already on its way, so a broker that dies mid-request would keep the relay, and its shutdown, waiting until the broker returned. The price is a possible duplicate, which at-least-once delivery already allows.

**Layering.** The franz-go wrapper is infrastructure (`pkg/infra/queue/kafka`), the port is `ports.EventPublisher` and its adapter `internal/adapters/kafka`; the relay is a use case (`internal/application/outbox`) behind `ports.OutboxDatastore`, whose adapter is `internal/adapters/postgres/outbox`.

**Traces continue through the outbox.** The writer stores `traceparent` and `tracestate` (never baggage, which can carry user data) in `outbox_events.headers`; the relay starts an `outbox.publish` span per record as a child of that context and sends that span's `traceparent` in the Kafka record, so one trace ID runs from the HTTP request to the consumer. The relay's own queries run under `telemetry.Unsampled`, otherwise a poll every 100 ms would start ten traces a second. The worker's service name is `fupi-worker`, so the API's dashboards and alerts stay about the API.

**Sent rows are purged.** The leader deletes rows sent more than `OUTBOX_RETENTION_DAYS` ago (default 7, `0` keeps them), in batches; unsent and parked rows are never deleted.

## Considered options

- **A librdkafka-based client** (`confluent-kafka-go`) — the most widely used, but it needs cgo, which the static distroless image does not have.
- **Several relays claiming rows with `FOR UPDATE SKIP LOCKED`** — more throughput and no failover wait, but events of one aggregate could be published out of order, and nothing here needs the throughput.
- **`LISTEN/NOTIFY` or change-data-capture instead of polling** — lower latency, but a second moving part (a lost notification still needs the poll as a safety net, CDC needs Debezium and Kafka Connect) to save latency nobody needs.
- **A dead-letter topic for rejected events** — it moves the problem to another place nobody watches; a parked row, a gauge and an alert keep it where the fix (and the original payload) is.
- **Delaying publication a little to follow commit order** — `created_at` is the transaction's start time, so a transaction that started earlier and committed later can be published after a later-started one. A visibility lag would narrow that window and cost latency on every event; each event carries `occurred_at` and a consumer that needs strict order sorts by it.

## Consequences

- Consumers must be idempotent on `event_id`.
- Order is per `partition_key` (the aggregate ID) and follows `(created_at, id)`, not commit order. For one aggregate the difference needs two concurrent writers, which the aggregate's row lock normally serializes; across aggregates no order is promised.
- If one partition's leader is unavailable and more than `OUTBOX_BATCH_SIZE` of a destination's oldest unsent rows belong to it, other keys of that destination wait until it recovers; the backlog alert shows it.
- The worker needs Postgres and Kafka; it never migrates (ADR-0014) and never creates topics (the stack's `kafka-init` does).
- A second worker is a standby, not extra capacity.
- There is no consumer yet; the first one (Analytics, cache invalidation) brings its own decisions.
````

- [ ] **Step 2: Update ADR-0011 and ADR-0017**

In `docs/adr/0011-single-broker-agnostic-outbox.md`, in the list of what each row stores, change the `headers` bullet to:

```markdown
- `headers`: `event_id`, `event_type`, `aggregate_id`, `occurred_at` and, when the request had a trace, `traceparent` (and `tracestate`), for the relay to forward as message headers.
```

and replace the consequence bullet `No relay exists yet, so rows accumulate unsent (...)` with:

```markdown
- The outbox relay (ADR-0018) delivers the rows to Kafka and records `sent_time`; a row the broker permanently rejects is parked (`parked_at`). The URL context adopts the same helper when its application layer lands.
```

In `docs/adr/0017-the-api-exports-telemetry-over-otlp-and-keeps-logs-on-stdout.md` change the last consequence bullet to end `... belong to the outbox relay slice (ADR-0018).` (only the last words change).

- [ ] **Step 3: Document the relay in `docs/LocalInfrastructure.md`**

Make these edits.

(a) In the `## Modes and profiles` table add a row after `Everything except legacy`:

```markdown
| Outbox relay worker (needs Postgres and Kafka) | `worker-ha` | `worker-single` | `infra.worker.up` |
```

(b) In the `## Make commands` table, in the first row, append ` worker` to the stack list (`... core full app worker`).

(c) Insert this section between `## The app in a container` and `## Observing the app`:

````markdown
## Relaying events to Kafka

The outbox relay is a second process, `worker`, built into the same image. It reads `outbox_events` from Postgres and publishes each row to the Kafka topic named by its `destination` (`identity.events`), keyed by `partition_key` so one aggregate's events stay in order. The API never talks to Kafka.

```bash
make infra.worker.up MODE=single   # or HA; brings up Postgres and Kafka for that mode first, then builds and starts the worker
curl -s localhost:8086/health/ready
make infra.worker.down             # stops only the worker; Postgres and Kafka keep running
```

- It listens on `127.0.0.1:8086` (`GET /health`, `GET /health/ready`). Its environment is an explicit list in `deploy/worker/compose.yml` (not your `.env`), it runs with `ENVIRONMENT=development`, a read-only filesystem and no capabilities, and its health check is `/app/worker healthcheck`.
- One worker is active at a time: they compete for a Postgres advisory lock and the others stand by, taking over within `OUTBOX_STANDBY_INTERVAL` seconds (default 5) after the active one stops. To try a failover, run a second worker on your host with `WORKER_HTTP_PORT=8087 go run ./app/cmd/worker` (the defaults reach `localhost:19092` and `localhost:5432`; in HA mode also set `KAFKA_BROKERS=localhost:19092,localhost:29092,localhost:39092`).
- Read what it published, headers included (`event_id`, `event_type`, `aggregate_id`, `occurred_at`, and `traceparent` when the request had a trace), or look at the topic in Kafka UI (<http://localhost:8080>):

```bash
docker compose --profile kafka-single exec kafka-single /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka-1:9092 --topic identity.events --from-beginning --timeout-ms 5000 \
  --property print.key=true --property print.headers=true
```

  In HA mode use `--profile kafka-ha exec kafka-1`.
- Delivery is **at least once**: a worker killed between a publish and recording it can publish the event again, so a consumer de-duplicates on the `event_id` header. Order is per `partition_key`.
- **Kafka down:** nothing is lost and nothing is parked. Events wait in the table, the worker backs off (200 ms up to 10 s) and `OutboxBacklogOld` fires when the oldest has waited more than five minutes; when Kafka returns they are published in order.
- **Parked events:** a record Kafka permanently refuses (for example one larger than the broker accepts) is retried until `OUTBOX_MAX_ATTEMPTS` (default 3) and then parked: `parked_at` and `error_message` are set, the rest keep flowing, and `OutboxEventsParked` fires after five minutes. Look at them and, once the cause is fixed, put one back:

```bash
make infra.psql MODE=single
```

```sql
SELECT id, destination, event_type, attempts, error_message, parked_at FROM outbox_events WHERE parked_at IS NOT NULL;
UPDATE outbox_events SET parked_at = NULL, attempts = 0, error_message = NULL WHERE id = '<id>';
```

- **Purge:** the active worker deletes rows sent more than `OUTBOX_RETENTION_DAYS` ago (default 7; `0` keeps them) when it starts and every ten minutes. Unsent and parked rows are never deleted.
- **Observing it:** the "Fupi worker" dashboard (folder Fupi) and the traces: each published event is a span `<destination> publish` of the service `fupi-worker`, a child of the request that wrote the event, so a registration's trace ends in the Kafka record's `traceparent`. Metrics are `outbox_relay_*` (published, failures, publish duration, backlog, oldest unsent age, parked rows, leader).
- Settings (all in `.env.example`): `KAFKA_BROKERS`, `KAFKA_CLIENT_ID`, `KAFKA_PUBLISH_TIMEOUT`, `OUTBOX_POLL_INTERVAL_MS`, `OUTBOX_BATCH_SIZE`, `OUTBOX_MAX_ATTEMPTS`, `OUTBOX_STANDBY_INTERVAL`, `OUTBOX_RETENTION_DAYS` and `WORKER_HTTP_PORT`.
````

(d) In `## Credentials and ports` add a row after the `Kafka UI` row:

```markdown
| Worker health | `localhost:8086` (`/health`, `/health/ready`) | none |
```

(e) In `## Failure drills (HA)` add a row after the Kafka row:

```markdown
| Outbox relay | stop the worker (`make infra.worker.down`) while a second one runs on your host | the second worker becomes the relay within `OUTBOX_STANDBY_INTERVAL` seconds; no event is lost (a duplicate carries the same `event_id`) |
```

- [ ] **Step 4: Document the worker in `docs/Deployment.md`**

(a) In `## Configuration`, in the sentence `In that mode the API and the migrator refuse the development secrets`, change `the API and the migrator` to `the API, the migrator and the worker`.

(b) Add this section after `## Running` (before `## Continuous integration`):

````markdown
## The outbox relay worker

The image also contains `/app/worker`, which delivers the transactional outbox to Kafka. Run it as its own container from the same image, after the migrator and with the same database settings as the API (it never migrates):

```bash
docker run -p 8086:8086 -e DATABASE_HOST=... -e DATABASE_NAME=... -e DATABASE_USERNAME=... -e DATABASE_PASSWORD=... -e KAFKA_BROKERS=... \
  --entrypoint /app/worker --name fupi-worker <IMAGE_NAME>:<IMAGE_TAG>
```

| Variable | Default | Meaning |
|---|---|---|
| `KAFKA_BROKERS` | `localhost:19092` | comma-separated `host:port` list; set it |
| `KAFKA_CLIENT_ID` | `fupi-worker` | |
| `KAFKA_PUBLISH_TIMEOUT` | `10` | seconds a record may take to be acknowledged before it counts as a transient failure |
| `OUTBOX_POLL_INTERVAL_MS` | `100` | milliseconds between cycles when nothing was claimed (at least 10) |
| `OUTBOX_BATCH_SIZE` | `100` | rows claimed per destination per cycle (1 to 1000) |
| `OUTBOX_MAX_ATTEMPTS` | `3` | permanent rejections before an event is parked (at least 1) |
| `OUTBOX_STANDBY_INTERVAL` | `5` | seconds between a standby's attempts to become the active relay |
| `OUTBOX_RETENTION_DAYS` | `7` | days sent rows are kept; `0` keeps them forever |
| `WORKER_HTTP_PORT` | `8086` | the health listener |

The database, logging and telemetry variables are the API's; the worker's service name defaults to `fupi-worker`.

- Run more than one for failover: one is active (it holds a Postgres advisory lock), the others stand by. A second worker is not extra capacity.
- The topics must exist (the relay does not create them); an event for a missing topic waits and shows in the backlog alert.
- `GET /health` is liveness: 503 when the relay loop has not completed a cycle or a standby attempt for 30 seconds, so a wedged worker is restarted. `GET /health/ready` is 503 when Postgres or Kafka is down or the worker is draining; neither is a reason to restart. The container's health check is `/app/worker healthcheck`.
- On SIGTERM the worker finishes the batch it has in flight (bounded by `KAFKA_PUBLISH_TIMEOUT` and `SHUTDOWN_TIMEOUT`), releases the lock, exports its last telemetry and exits 0. Give it the same stop grace period as the API (25 seconds locally).
- Delivery is at least once: consumers must de-duplicate on the `event_id` header. See ADR-0018 and the operations notes in `docs/LocalInfrastructure.md` (parked events, purge, failure behaviour).
````

- [ ] **Step 5: Check the documentation**

Run: `grep -c 'worker' docs/LocalInfrastructure.md docs/Deployment.md; grep -n 'No relay exists yet' docs/adr/0011-single-broker-agnostic-outbox.md; ls docs/adr | tail -2`
Expected: both docs list matches, no output from the `No relay exists yet` search, and `0018-the-outbox-relay-is-one-leader-elected-polling-worker-over-franz-go.md` is the last file.

Run: `bash scripts/infra_env_check.sh && echo env-check-ok`
Expected: `env-check-ok`.

Read the new section once as a reader who has not seen the code: every command must run as written (the `kafka-console-consumer.sh` path is checked in Task 16).

- [ ] **Step 6: Commit**

```bash
git add docs
git commit -m "docs: document the outbox relay and record ADR-0018"
```

---

### Task 16: The live drill and the final checks

**Files:** none are planned; fix what the drill finds in the owning task's files, test-first, and say so in the ledger.

**Interfaces:**
- Consumes: the whole branch.
- Produces: evidence for the spec's success criteria 1 to 7 on a running stack, and the confirmed Prometheus metric names.

This task runs real stacks. Before starting: `docker info` (start Docker Desktop if it is not running), `docker ps` (the developer's own containers must not be disturbed, and nothing here may stop them), and do not touch `.env` or `/etc/hosts`. Drill rows are tagged `event_type = 'drill.event'` and removed at the end. The go-ahead of Task 6 covers the image builds.

- [ ] **Step 1: Start the stacks**

Run, in this order:

```bash
make infra.observability.up
make infra.worker.up MODE=single
make infra.app.up MODE=single
docker compose --profile worker-single ps worker-single
curl -s localhost:8086/health/ready; echo; curl -s localhost:8085/health/ready; echo
```

Expected: the worker container is `healthy`; both ready calls answer `{"status":"ok",...}` with `postgres` and `kafka` (worker) or `postgres` and `redis` (API) up. If the machine's memory allows, also `make infra.elk.up MODE=single` for the log side of the trace (the spec's drill uses it); skip it, and say so in the ledger, when it does not.

- [ ] **Step 2: (a) A registration reaches `identity.events` with its trace**

```bash
curl -s -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' -H 'content-type: application/json' \
  -d '{"username":"drill-one","first_name":"Drill","email":"drill-one@example.com","password":"Drill-Passw0rd-1"}' \
  localhost:8085/api/v1/fupi/auth/register
docker compose --profile kafka-single exec kafka-single /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka-1:9092 --topic identity.events --from-beginning --timeout-ms 5000 \
  --property print.key=true --property print.headers=true 2>/dev/null | grep drill-one
make infra.psql MODE=single   # then: SELECT id, sent_time, attempts, parked_at FROM outbox_events ORDER BY created_at DESC LIMIT 3;
```

Expected: the registration answers 2xx; exactly one record with the user's ID as key, headers `event_id`, `event_type=user.registered`, `aggregate_id`, `occurred_at` and a `traceparent` whose trace ID is `4bf92f3577b34da6a3ce929d0e0e4736` (the publish span is a child in that trace); the row has a `sent_time`. In Grafana (<http://localhost:3000>), Explore, Tempo, trace ID `4bf92f3577b34da6a3ce929d0e0e4736`: the HTTP span, the `identity.Register` span, the Postgres spans and a `identity.events publish` span of `fupi-worker`. If `docker compose exec` cannot find the service by that name, `docker compose --profile '*' ps` shows the name to use; correct the command in Task 15's section too.

- [ ] **Step 3: Confirm the Prometheus names of the relay metrics**

```bash
curl -s 'localhost:9090/api/v1/label/__name__/values' | python3 -c "import json,sys;print('\n'.join(n for n in json.load(sys.stdin)['data'] if n.startswith('outbox')))"
```

Expected: `outbox_relay_published_total`, `outbox_relay_backlog`, `outbox_relay_oldest_unsent_age_seconds`, `outbox_relay_parked_rows`, `outbox_relay_leader` and `outbox_relay_publish_duration_seconds_bucket`/`_count`/`_sum` (`outbox_relay_failures_total` appears after the first failure, in step 4). The metric export interval is 15 s, so wait a little after step 2. If any name differs from the one the alert rules, the dashboard JSON and Task 15's text use, correct all three together (rules and dashboard first, the rules' unit tests in `stack_test.yml` use the same names), re-run Task 14's promtool commands, and commit `fix(observability): use the metric names the stack really exposes`. Check the dashboard "Fupi worker" renders data in all nine panels.

- [ ] **Step 4: (b) Kafka stopped and started again**

```bash
docker compose --profile kafka-single stop kafka-single
for i in 1 2 3 4 5; do curl -s -o /dev/null -w '%{http_code} ' -H 'content-type: application/json' \
  -d "{\"username\":\"drill-out-$i\",\"first_name\":\"Drill\",\"email\":\"drill-out-$i@example.com\",\"password\":\"Drill-Passw0rd-1\"}" \
  localhost:8085/api/v1/fupi/auth/register; done; echo
curl -s localhost:8086/health; echo; curl -s localhost:8086/health/ready; echo
```

Expected: five 2xx (the API does not need Kafka); the worker's `/health` stays 200 and `/health/ready` is 503 with `kafka` down; the worker container stays up and is not restarted (`docker compose --profile worker-single ps`); the five rows have no `sent_time` and `attempts = 0`, `parked_at` null (outage never counts). Then:

```bash
docker compose --profile kafka-single start kafka-single
sleep 40
docker compose --profile kafka-single exec kafka-single /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka-1:9092 --topic identity.events --from-beginning --timeout-ms 8000 \
  --property print.key=true 2>/dev/null | grep -c drill-out
curl -s 'localhost:9090/api/v1/query?query=max(outbox_relay_backlog)' | python3 -m json.tool | grep -A1 '"value"'
```

Expected: `5` records (one per registration, in registration order: check the order of the `drill-out-N` users in the output), the backlog gauge back to 0, and every row sent. While Kafka was down the expression `max(outbox_relay_oldest_unsent_age_seconds{service_name="fupi-worker"})` was above zero in Prometheus (the alert itself needs five minutes above 300 seconds: evaluating the expression is enough here).

- [ ] **Step 5: (c) The worker killed in the middle of a batch**

Insert a few thousand rows and kill the worker while it publishes them:

```bash
make infra.psql MODE=single
```

```sql
WITH ids AS (SELECT gen_random_uuid() AS id, g FROM generate_series(1, 5000) g)
INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload)
SELECT id, 'drill-' || (g % 10), 'identity.events', 'drill.event', json_build_object('event_id', id::text), json_build_object('n', g) FROM ids;
```

Then, from another terminal, straight away:

```bash
docker kill fupi-worker-single-1 && docker compose --profile worker-single up -d worker-single
sleep 30
make infra.psql MODE=single   # SELECT count(*) FILTER (WHERE sent_time IS NULL) AS unsent, count(*) AS total FROM outbox_events WHERE event_type = 'drill.event';
docker compose --profile kafka-single exec kafka-single /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka-1:9092 --topic identity.events --from-beginning --timeout-ms 15000 \
  --property print.headers=true 2>/dev/null | grep drill.event | grep -o 'event_id:[0-9a-f-]*' | sort | uniq -c | awk '{c[$1]++} END {for (k in c) print k" time(s): "c[k]" events"}'
```

(If the container name differs, `docker compose --profile worker-single ps --format '{{.Name}}'` shows it.) Expected: `unsent = 0` and `total = 5000`; the last command prints `1 time(s): <N> events` and, only if the kill came mid-batch, `2 time(s): <M> events` with `N + M = 5000` and nothing at 0: every event is delivered at least once, a duplicate repeats its `event_id`. If the kill landed before the first claim or after the last mark (nothing duplicated), repeat with a larger batch; the point is that no event is missing.

- [ ] **Step 6: (d) Two workers: one publishes, the other takes over**

Run a second worker on the host (single mode defaults reach `localhost:19092` and `localhost:5432`):

```bash
WORKER_HTTP_PORT=8087 go run ./app/cmd/worker
```

(a terminal of its own). Expected: it stays quiet and `curl -s localhost:8087/health/ready` answers 200; in Postgres `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted;` is `1` (only the container holds the lock) and `curl -s 'localhost:9090/api/v1/query?query=sum(outbox_relay_leader)'` is 1. Now:

```bash
make infra.worker.down
sleep 12
```

then register a user or insert a drill row as in step 5 (one row is enough). Expected: the host worker's log shows it acquired the lease, the row is sent and appears on the topic, and `sum(outbox_relay_leader)` is 1 again. Stop the host worker with Ctrl-C (it exits 0, logging `shutting down the worker`) and start the container again: `make infra.worker.up MODE=single`.

- [ ] **Step 7: (e) A poison event is parked and the rest still flow**

```sql
INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload)
VALUES (gen_random_uuid(), 'drill-poison', 'identity.events', 'drill.poison', '{}'::json, json_build_object('big', repeat('x', 2000000)));
```

then register a user (any), wait 10 seconds and look:

```sql
SELECT event_type, attempts, parked_at IS NOT NULL AS parked, left(error_message, 80) FROM outbox_events WHERE event_type IN ('drill.poison', 'user.registered') ORDER BY created_at DESC LIMIT 3;
```

Expected: the poison row has `attempts = 3`, `parked = t` and an error message naming the oversized record; the registration after it is sent. `curl -s 'localhost:9090/api/v1/query?query=outbox_relay_parked_rows'` shows 1 (and `OutboxEventsParked` would fire after five minutes: the expression is enough). Re-queue it:

```sql
UPDATE outbox_events SET parked_at = NULL, attempts = 0, error_message = NULL WHERE event_type = 'drill.poison';
```

Expected: within a few seconds it is parked again with `attempts = 3` (it is still too big), which shows both that the re-queue is picked up and that it does not wedge the relay.

- [ ] **Step 8: (f) The purge deletes only old sent rows**

```sql
UPDATE outbox_events SET sent_time = now() - interval '10 days' WHERE event_type = 'drill.event' AND sent_time IS NOT NULL;
SELECT count(*) FROM outbox_events WHERE event_type = 'drill.event';   -- 5000
```

Restart the worker (the active worker purges when it starts): `docker compose --profile worker-single restart worker-single`, wait 15 seconds, then run the `SELECT count(*)` again.

Expected: `0` left of the backdated drill rows; the worker's log has `outbox relay: purged sent events` with `"deleted":5000`; the parked poison row, the unsent rows (none now) and the recent registrations are all still there.

- [ ] **Step 9: (g) SIGTERM exits 0 and flushes**

```bash
docker compose --profile worker-single stop worker-single
docker compose --profile worker-single ps -a --format '{{.Name}} {{.Status}}'
docker compose --profile worker-single logs --tail 8 worker-single
```

Expected: the container exited with code 0 well within 25 seconds (`Exited (0)`), and the log's last lines are `shutting down the worker` and no error. In Grafana the worker's last spans are present. Start it again with `make infra.worker.up MODE=single`.

- [ ] **Step 10: Clean up the drill rows and, optionally, check HA**

```sql
DELETE FROM outbox_events WHERE event_type IN ('drill.event', 'drill.poison');
```

(the registrations made by the drill are ordinary users; leave them, or remove them through `make infra.psql` with `DELETE FROM users WHERE username LIKE 'drill-%';` after `DELETE FROM outbox_events WHERE partition_key IN (SELECT id::text FROM users WHERE username LIKE 'drill-%');`.)

Optional, only if `docker stats --no-stream` and the machine's memory allow it (HA runs Patroni behind HAProxy and three brokers): `make infra.worker.down`, `make infra.worker.up MODE=ha`, register a user through `make infra.app.up MODE=ha`, and confirm the record arrives and `/health/ready` reports both dependencies up: that the advisory lock works through HAProxy and the producer sees all brokers. Say in the ledger whether this was done or skipped for memory. Then return to the single stacks or stop what the drill started (`make infra.worker.down`, `make infra.app.down`), and leave the developer's own containers as they were.

- [ ] **Step 11: Fix what the drill found**

Anything that did not behave as expected is a defect in an earlier task or a wrong line in this plan: write the failing test first (unit, adapter or integration, in the owning task's file), fix it, run the whole suite, and commit as `fix(outbox): ...` with the trailer. Record each finding in the ledger as `Task 16: found <what> — fixed in <file>, test <name> RED→GREEN`, or as a `Ruling:` when the spec or the plan was wrong.

- [ ] **Step 12: Final checks**

Run each and read the output:

```bash
go build ./... && go vet ./app/... && go vet -tags integration ./app/...
gofmt -l app/cmd app/api/probes app/config/worker.go app/config/worker_test.go app/internal app/pkg/infra/queue app/pkg/infra/telemetry app/test
go test -count=1 ./... 2>&1 | tail -30
go test -race -count=1 ./app/internal/application/outbox ./app/internal/adapters/kafka ./app/pkg/infra/queue/kafka ./app/pkg/infra/telemetry ./app/api/probes ./app/cmd/worker ./app/config
~/go/bin/golangci-lint-v2 run ./app/... 2>&1 | tail -15
bash scripts/infra_test.sh 2>&1 | tail -2
make infra.config 2>&1 | tail -2
make lint.docker 2>&1 | tail -2
git status --short
```

Expected: build and both vets clean; no gofmt output (the old files named in the plan notes are not listed); every package `ok`; the race run `ok`; golangci-lint reports only the two findings that already exist in `sequence_short_code_gen.go`; `all tests passed`; `ok: all profiles`; `Done linting Dockerfile`; `git status` empty. `make lint.workflows` needs the `rhysd/actionlint:1.7.12` image: run it only if `docker images` shows it locally, otherwise say it was not run (no workflow file changed). Then the integration suites, which need Docker:

```bash
go test -tags integration -count=1 -timeout 30m ./app/pkg/infra/queue/kafka ./app/internal/adapters/postgres/... ./app/internal/application/outbox ./app/cmd/worker ./app/pkg/infra/database/postgres 2>&1 | tail -15
```

Expected: every package `ok`.

- [ ] **Step 13: Final whole-branch review**

Per the executing-plans skill: build the review package (`review-package PLAN MERGE_BASE HEAD`, with `MERGE_BASE=$(git merge-base feat/local-infra-stack HEAD)`: this branch forks from slices 1 to 4, not from `main`), dispatch the reviewer on the most capable model with this plan's Review Focus verbatim and the ledger's `Ruling:` lines, and handle its findings by the skill's rules (one fix pass, test first; minors to the ledger and the final message). Then finish with the finishing-a-development-branch skill.

---

## Spec coverage

| Spec item | Task |
|---|---|
| Success criterion 1 (a registration appears once on `identity.events` with key, payload, headers, `traceparent`; row has `sent_time`) | Tasks 2, 4, 5, 7, 12 (`..._DeliversWrittenEvents...`), 16 step 2 |
| Criterion 2 (Kafka down: API unaffected, rows wait, published in order afterwards) | Tasks 4 (backoff, no counting), 6 (outage in miniature), 12 (`..._DrainsTheBacklogInOrder...`), 16 step 4 |
| Criterion 3 (worker killed mid-batch: at least once, duplicates repeat `event_id`) | Tasks 4 (batch in flight at shutdown), 11, 16 step 5 |
| Criterion 4 (two workers: one publishes, the other takes over) | Tasks 5 (lease), 12 (`..._OnlyOneOfTwoRelays...`), 16 step 6 |
| Criterion 5 (permanent rejection parked after the attempts; the rest flows) | Tasks 4, 5, 7 (classification), 12 (`..._ParksAnEvent...`), 16 step 7 |
| Criterion 6 (purge of old sent rows only) | Tasks 4 (cadence), 5 (`TestPurge_...`), 12 (`..._PurgesSentRows...`), 16 step 8 |
| Criterion 7 (one trace ID from request through `outbox.publish` to the record) | Tasks 2 (writer), 3 (message), 4 (spans), 12, 16 step 2 |
| Criterion 8 (health check, compose stack, image, docs, green checks) | Tasks 10, 11, 13, 14, 15, 16 step 12 |
| D1 franz-go | Task 6 |
| D2 one active relay by advisory lock | Tasks 4, 5, 12 |
| D3 parking, transient never parks | Tasks 4, 5, 7, 12 |
| D4 purge | Tasks 4, 5, 12 |
| D5 health endpoint and `worker` stack | Tasks 10, 11, 13 |
| D6 no consumer | (nothing is built; the test broker's `Consume` helper is test-only) |
| D7 at least once | Tasks 4, 15 (ADR-0018) |
| D8 polling | Tasks 4, 11 (`relayConfig`) |
| D9 `traceparent`/`tracestate` stored, publish span, no baggage | Tasks 2, 3, 4 |
| D10 layering | Tasks 3, 5, 6, 7 |
| D11 service name `fupi-worker` | Tasks 8, 11, 13 |
| D12 housekeeping under `telemetry.Unsampled` | Task 4 |
| D13 the worker never migrates | Tasks 1 (migration 000003 through the migrator), 11, 15 |
| D14 commit trailer | Global Constraints |
| Data and queries (migration 000003, sqlc queries, writer change) | Tasks 1, 2, 5 |
| Kafka message and producer (headers, key, errors, bounded Produce) | Tasks 3, 6, 7 |
| Worker (config, startup, health, shutdown) | Tasks 8, 9, 10, 11 |
| Observability (metrics, two alerts, dashboard) | Tasks 4, 14, 16 step 3 |
| Compose, image, docs, ADR-0018, ADR-0011 update | Tasks 13, 15 |
| Verification plan (unit, integration, live drill, repository checks) | every task's tests, Task 16 |
