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
