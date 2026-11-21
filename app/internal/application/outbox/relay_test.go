package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// The reason is stored in a VARCHAR column, and PostgreSQL rejects text that is not valid UTF-8: a reason cut in the middle
// of a character would make the rejection fail to record and the event retry for ever without being counted or parked.
func TestTruncate_NeverSplitsACharacter(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 500))

	got := truncate(strings.Repeat("é", 10), 5) // each é is two bytes, so a cut at byte 5 is inside one
	assert.True(t, utf8.ValidString(got), "%q is valid UTF-8", got)
	assert.Equal(t, "éé", got)

	got = truncate("日本語のエラー", 7) // three bytes per character
	assert.True(t, utf8.ValidString(got), "%q is valid UTF-8", got)
	assert.LessOrEqual(t, len(got), 7)
}
