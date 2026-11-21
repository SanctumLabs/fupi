package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// limiterTelemetry is a limited app wired to an SDK meter provider and a span recorder, so a test reads back what would
// have been exported.
type limiterTelemetry struct {
	app    *fiber.App
	reader *sdkmetric.ManualReader
	spans  *tracetest.SpanRecorder
}

func newLimiterTelemetry(t *testing.T, fake *fakeAllower) *limiterTelemetry {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	spans := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	t.Cleanup(func() {
		_ = meterProvider.Shutdown(context.Background())
		_ = tracerProvider.Shutdown(context.Background())
	})

	cfg := redisLimitedConfig(fake, newTestClock(), 5)
	cfg.MeterProvider = meterProvider
	handler, err := RateLimiterMiddleware(cfg)
	require.NoError(t, err)

	app := fiber.New()
	// A parent span on the user context, like the one the OTel middleware sets.
	app.Use(func(c *fiber.Ctx) error {
		ctx, span := tracerProvider.Tracer("test").Start(c.UserContext(), "request")
		defer span.End()
		c.SetUserContext(ctx)
		return c.Next()
	})
	app.Use(handler)
	app.All("/*", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	return &limiterTelemetry{app: app, reader: reader, spans: spans}
}

func (l *limiterTelemetry) collect(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, l.reader.Collect(context.Background(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	assert.Equal(t, "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware", rm.ScopeMetrics[0].Scope.Name)

	byName := map[string]metricdata.Metrics{}
	for _, m := range rm.ScopeMetrics[0].Metrics {
		byName[m.Name] = m
	}
	return byName
}

// counts flattens a counter into its value per attribute set, written as "k=v,k=v" in key order.
func counts(t *testing.T, m metricdata.Metrics) map[string]int64 {
	t.Helper()

	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "%s is a counter", m.Name)
	out := map[string]int64{}
	for _, dp := range sum.DataPoints {
		out[dp.Attributes.Encoded(attribute.DefaultEncoder())] = dp.Value
	}
	return out
}

func gaugeValue(t *testing.T, m metricdata.Metrics) int64 {
	t.Helper()

	gauge, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "%s is a gauge", m.Name)
	require.Len(t, gauge.DataPoints, 1)
	return gauge.DataPoints[0].Value
}

func TestRateLimiterMetrics_CountsDecisions(t *testing.T) {
	fake := &fakeAllower{result: &redis_rate.Result{Allowed: 1, Remaining: 4}}
	l := newLimiterTelemetry(t, fake)

	assert.Equal(t, fiber.StatusOK, serve(t, l.app, "/x", testPeer, nil).StatusCode())
	fake.answer(&redis_rate.Result{Allowed: 0, RetryAfter: time.Second}, nil)
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, l.app, "/x", testPeer, nil).StatusCode())

	metrics := l.collect(t)
	assert.Equal(t, map[string]int64{
		"backend=redis,group=auth,outcome=allowed": 1,
		"backend=redis,group=auth,outcome=limited": 1,
	}, counts(t, metrics["http.ratelimit.requests"]))
	assert.EqualValues(t, 0, gaugeValue(t, metrics["http.ratelimit.fallback.active"]))
	assert.NotContains(t, metrics, "http.ratelimit.backend.errors", "no error, no data point")
}

func TestRateLimiterMetrics_FallbackSignals(t *testing.T) {
	l := newLimiterTelemetry(t, &fakeAllower{err: errRedisDown})

	assert.Equal(t, fiber.StatusOK, serve(t, l.app, "/x", testPeer, nil).StatusCode())

	metrics := l.collect(t)
	assert.Equal(t, map[string]int64{"group=auth": 1}, counts(t, metrics["http.ratelimit.backend.errors"]))
	assert.EqualValues(t, 1, gaugeValue(t, metrics["http.ratelimit.fallback.active"]))
	assert.Equal(t, map[string]int64{"backend=memory,group=auth,outcome=allowed": 1},
		counts(t, metrics["http.ratelimit.requests"]))
}

func TestRateLimiterMetrics_MemoryOnlyCountsWithoutFallback(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	app := newLimitedApp(t, RateLimiterConfig{
		Groups:        []RateLimitGroup{{Name: "auth", Rule: RateLimitRule{Max: 1, Window: time.Minute}}},
		MeterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	})

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/x", testPeer, nil).StatusCode())

	l := &limiterTelemetry{reader: reader}
	metrics := l.collect(t)
	assert.Equal(t, map[string]int64{
		"backend=memory,group=auth,outcome=allowed": 1,
		"backend=memory,group=auth,outcome=limited": 1,
	}, counts(t, metrics["http.ratelimit.requests"]))
	assert.EqualValues(t, 0, gaugeValue(t, metrics["http.ratelimit.fallback.active"]), "no Redis, no fallback")
}

func TestRateLimiterSpan_Attributes(t *testing.T) {
	l := newLimiterTelemetry(t, &fakeAllower{result: &redis_rate.Result{Allowed: 1, Remaining: 4}})

	serve(t, l.app, "/x", testPeer, nil)

	ended := l.spans.Ended()
	require.Len(t, ended, 1)
	attrs := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	assert.Equal(t, "auth", attrs["ratelimit.group"].AsString())
	assert.Equal(t, "auth:ip:"+testPeer, attrs["ratelimit.key"].AsString())
	assert.Equal(t, "redis", attrs["ratelimit.backend"].AsString())
	assert.False(t, attrs["ratelimit.limited"].AsBool())
	assert.EqualValues(t, 4, attrs["ratelimit.remaining"].AsInt64())
}

func TestRateLimiterSpan_FallbackReadsRemainingFromHeader(t *testing.T) {
	l := newLimiterTelemetry(t, &fakeAllower{err: errRedisDown})

	serve(t, l.app, "/x", testPeer, nil)

	ended := l.spans.Ended()
	require.Len(t, ended, 1)
	attrs := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	assert.Equal(t, "memory", attrs["ratelimit.backend"].AsString())
	assert.False(t, attrs["ratelimit.limited"].AsBool())
	assert.EqualValues(t, 4, attrs["ratelimit.remaining"].AsInt64())
}
