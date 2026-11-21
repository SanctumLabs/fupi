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
