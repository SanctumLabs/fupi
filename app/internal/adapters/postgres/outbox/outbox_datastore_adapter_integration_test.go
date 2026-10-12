//go:build integration

package outboxdatastore_test

import (
	"context"
	"encoding/json"
	"os"
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

func TestLeaser_LeasesWithDifferentNamesAreIndependent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	expiry, err := outboxdatastore.NewLeaser(f.pool.Config().ConnString(), "fupi.url.expiry", "fupi-url-expiry-lease",
		10*time.Second, outboxdatastore.DefaultLeaseIdleTimeout)
	require.NoError(t, err)

	relay, err := f.adapter.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(relay.Release)

	held, err := expiry.Acquire(ctx)
	require.NoError(t, err, "the relay's lease does not block the expiry job's")
	t.Cleanup(held.Release)

	_, err = expiry.Acquire(ctx)
	assert.ErrorIs(t, err, ports.ErrNotLeader, "a second expiry job does not get its lease")
}

// A holder whose host or network dies without closing the connection (no FIN, no RST) leaves a backend that never notices:
// without a server-side limit it would keep the lock for hours and no standby could take over. The lease connection asks the
// server to drop it after it has sat idle for a while; a holder that checks its lease regularly is never idle that long.
func TestLease_ASessionThatStopsCheckingItsLeaseLosesItOnTheServer(t *testing.T) {
	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)
	adapter, err := outboxdatastore.NewAdapter(client, client.GetDB().Config().ConnString(), 10*time.Second,
		outboxdatastore.WithLeaseIdleTimeout(time.Second))
	require.NoError(t, err)

	silent, err := adapter.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(silent.Release)

	var holders int
	require.NoError(t, client.GetDB().QueryRow(ctx,
		"SELECT count(*) FROM pg_stat_activity WHERE application_name = 'fupi-outbox-lease'").Scan(&holders))
	assert.Equal(t, 1, holders, "the lease connection can be told apart from the others")

	require.Eventually(t, func() bool {
		other, acquireErr := adapter.Acquire(ctx)
		if acquireErr != nil {
			return false
		}
		other.Release()
		return true
	}, 10*time.Second, 250*time.Millisecond, "the server dropped the idle lease connection, so another instance can take over")
	assert.Error(t, silent.Alive(ctx), "and the silent holder finds out the next time it checks")
}

func TestLease_AHolderThatKeepsCheckingKeepsItPastTheIdleTimeout(t *testing.T) {
	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)
	adapter, err := outboxdatastore.NewAdapter(client, client.GetDB().Config().ConnString(), 10*time.Second,
		outboxdatastore.WithLeaseIdleTimeout(time.Second))
	require.NoError(t, err)

	held, err := adapter.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(held.Release)

	for i := 0; i < 8; i++ { // 3.2 s, more than three idle timeouts, with a check every 400 ms
		time.Sleep(400 * time.Millisecond)
		require.NoError(t, held.Alive(ctx), "check %d", i)
	}
	_, err = adapter.Acquire(ctx)
	assert.ErrorIs(t, err, ports.ErrNotLeader, "a holder that keeps checking is never dropped")
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

// The destinations are found with a skip scan that compares names with > while the index orders them: names that differ
// by case, share a prefix or contain dots and dashes must each be visited exactly once, none skipped and none repeated.
func TestClaim_VisitsEveryDestinationOnceWhateverItsName(t *testing.T) {
	f := newFixture(t)
	destinations := []string{"a", "a.b", "a-b", "A", "B", "b", "identity.events", "url.events", "z"}
	want := map[string]string{}
	for i, destination := range destinations {
		want[destination] = f.insert(t, destination, "key", base.Add(time.Duration(i)*time.Second))
	}

	events, err := f.adapter.Claim(context.Background(), 5)
	require.NoError(t, err)

	got := map[string]string{}
	for _, e := range events {
		_, repeated := got[e.Destination]
		assert.False(t, repeated, "destination %q is claimed once", e.Destination)
		got[e.Destination] = e.ID
	}
	assert.Equal(t, want, got)
}

// claimStatement is the claim query exactly as it is written in the sqlc source, with its argument as $1.
func claimStatement(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../sql/queries/outbox/outbox_relay_queries.sql")
	require.NoError(t, err)
	_, rest, found := strings.Cut(string(raw), "-- name: QueryClaimOutboxEvents :many")
	require.True(t, found)
	body, _, _ := strings.Cut(rest, "-- name:")
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	statement := strings.TrimSpace(strings.Join(lines, "\n"))
	statement = strings.TrimSuffix(statement, ";")
	statement = strings.ReplaceAll(statement, "sqlc.arg(per_destination)", "$1")
	require.NotContains(t, statement, "sqlc.", "every sqlc argument is replaced")
	return statement
}

// rowsScanned adds up the rows every scan of outbox_events in an EXPLAIN (ANALYZE, FORMAT JSON) plan produced.
func rowsScanned(node map[string]any) float64 {
	var total float64
	if relation, _ := node["Relation Name"].(string); relation == "outbox_events" {
		rows, _ := node["Actual Rows"].(float64)
		loops, _ := node["Actual Loops"].(float64)
		total += rows * loops
	}
	if children, ok := node["Plans"].([]any); ok {
		for _, child := range children {
			total += rowsScanned(child.(map[string]any))
		}
	}
	return total
}

// After an outage the table can hold a very large backlog, and every claim must stay cheap: the work of one claim depends
// on the batch size and the number of destinations, not on how many events are waiting. A claim that reads the whole
// backlog to hand out one batch makes draining it quadratic and, past a size, slower than the statement timeout.
func TestClaim_ReadsOnlyTheRowsItReturnsHoweverLargeTheBacklogIs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.exec(t, `INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload, created_at)
		SELECT gen_random_uuid(), 'key-' || (g % 50), 'dest-' || (g % 3), 'user.registered', '{}'::json, '{}'::json,
		       now() - make_interval(secs => 30000 - g)
		FROM generate_series(1, 30000) g`)
	f.exec(t, "ANALYZE outbox_events")

	var plan []byte
	require.NoError(t, f.pool.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+claimStatement(t), int32(10)).Scan(&plan))
	var explained []map[string]any
	require.NoError(t, json.Unmarshal(plan, &explained))
	require.Len(t, explained, 1)

	scanned := rowsScanned(explained[0]["Plan"].(map[string]any))
	const batch, destinations = 10, 3
	assert.LessOrEqual(t, scanned, float64(4*batch*(destinations+1)),
		"a claim of %d events from %d destinations read %.0f of the 30000 waiting rows", batch, destinations, scanned)

	events, err := f.adapter.Claim(ctx, batch)
	require.NoError(t, err)
	assert.Len(t, events, batch*destinations, "and it still hands out a full batch of every destination")
}
