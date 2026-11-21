package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
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

// hasGauge reports whether the metric was exported with at least one data point.
func hasGauge(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[float64]); ok && m.Name == name && len(gauge.DataPoints) > 0 {
				return true
			}
		}
	}
	return false
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
	for _, name := range []string{"outbox.relay.backlog", "outbox.relay.parked_rows", "outbox.relay.oldest_unsent_age"} {
		assert.False(t, hasGauge(after, name), "%s is not reported once the leadership is gone: a zero would read as an empty backlog", name)
	}
	assert.Equal(t, int64(2), counterValue(t, after, "outbox.relay.published", attribute.String("destination", "identity.events")))
	assert.Equal(t, int64(0), counterValue(t, after, "outbox.relay.published", attribute.String("destination", "url.events")))
	assert.Equal(t, int64(1), counterValue(t, after, "outbox.relay.failures", attribute.String("kind", "transient")))
	assert.Equal(t, int64(1), counterValue(t, after, "outbox.relay.failures", attribute.String("kind", "permanent")))
}

// While no instance leads, a standby that reported a backlog of zero would hide a stuck outbox from the alerts that read
// the backlog gauges: it reports only that it is not the leader, and the backlog series are absent.
func TestRelay_AStandbyReportsOnlyThatItIsNotTheLeader(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	store := &fakeStore{
		acquireErrs: []error{ports.ErrNotLeader, ports.ErrNotLeader},
		backlog:     ports.Backlog{Unsent: 7, OldestUnsent: time.Now().Add(-time.Hour), Parked: 2},
	}
	h := newHarness(t, store, &fakePublisher{}, testConfig(), 2, WithMeterProvider(provider))

	var standby metricdata.ResourceMetrics
	baseSleep := h.relay.sleep
	h.relay.sleep = func(ctx context.Context, d time.Duration) {
		require.NoError(t, reader.Collect(context.Background(), &standby))
		baseSleep(ctx, d)
	}

	h.run(t)

	assert.Equal(t, 0.0, gaugeValue(t, standby, "outbox.relay.leader"))
	for _, name := range []string{"outbox.relay.backlog", "outbox.relay.parked_rows", "outbox.relay.oldest_unsent_age"} {
		assert.False(t, hasGauge(standby, name), "a standby does not report %s", name)
	}
}

// failingMeter makes one counter fail to be created and hands out working instruments for everything else.
type failingMeter struct {
	metric.Meter
	failOn string
}

func (m failingMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if name == m.failOn {
		return nil, errors.New("cannot create " + name)
	}
	return m.Meter.Int64Counter(name, opts...)
}

type failingMeterProvider struct {
	metricnoop.MeterProvider
	failOn string
}

func (p failingMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return failingMeter{Meter: p.MeterProvider.Meter(name, opts...), failOn: p.failOn}
}

// A counter that cannot be created must fail NewRelay: a nil counter would panic the first time a batch is delivered.
func TestNewRelay_AnInstrumentThatCannotBeCreatedIsAnErrorNotANilCounter(t *testing.T) {
	_, err := NewRelay(&fakeStore{}, &fakePublisher{}, testConfig(),
		WithMeterProvider(failingMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), failOn: "outbox.relay.published"}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox.relay.published")
}
