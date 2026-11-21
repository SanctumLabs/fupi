package health

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func up(context.Context) error   { return nil }
func down(context.Context) error { return errors.New("connection refused") }

func TestRegistry_StatusFollowsWhichChecksAreDown(t *testing.T) {
	cases := map[string]struct {
		checks     []Check
		wantStatus Status
		wantChecks map[string]string
		wantReady  bool
	}{
		"no checks":             {nil, StatusOK, map[string]string{}, true},
		"all up":                {[]Check{{"postgres", true, up}, {"redis", false, up}}, StatusOK, map[string]string{"postgres": "up", "redis": "up"}, true},
		"optional down":         {[]Check{{"postgres", true, up}, {"redis", false, down}}, StatusDegraded, map[string]string{"postgres": "up", "redis": "down"}, true},
		"required down":         {[]Check{{"postgres", true, down}, {"redis", false, up}}, StatusUnavailable, map[string]string{"postgres": "down", "redis": "up"}, false},
		"required and optional": {[]Check{{"postgres", true, down}, {"redis", false, down}}, StatusUnavailable, map[string]string{"postgres": "down", "redis": "down"}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry(time.Second)
			for _, check := range tc.checks {
				registry.Add(check)
			}

			report := registry.Run(context.Background())

			assert.Equal(t, tc.wantStatus, report.Status)
			assert.Equal(t, tc.wantChecks, report.Checks)
			assert.Equal(t, tc.wantReady, report.Ready())
		})
	}
}

// A dependency that never answers and ignores its context must not hang the readiness endpoint.
func TestRegistry_AHangingCheckCannotHangTheReport(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	registry := NewRegistry(50 * time.Millisecond)
	registry.Add(Check{Name: "postgres", Required: true, Fn: func(context.Context) error { <-release; return nil }})

	start := time.Now()
	report := registry.Run(context.Background())

	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, StatusUnavailable, report.Status)
	assert.Equal(t, "down", report.Checks["postgres"])
}

// The checks run in parallel: each waits until the other has started, so a sequential runner would time out.
func TestRegistry_RunsTheChecksInParallel(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	waiting := func(ctx context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	registry := NewRegistry(5 * time.Second)
	registry.Add(Check{Name: "a", Required: true, Fn: waiting})
	registry.Add(Check{Name: "b", Required: false, Fn: waiting})

	reports := make(chan Report, 1)
	go func() { reports <- registry.Run(context.Background()) }()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("the second check did not start while the first was still running")
		}
	}
	close(release)

	assert.Equal(t, StatusOK, (<-reports).Status)
}

func TestRegistry_DrainingReportsUnavailableWithoutRunningTheChecks(t *testing.T) {
	var calls atomic.Int32
	registry := NewRegistry(time.Second)
	registry.Add(Check{Name: "postgres", Required: true, Fn: func(context.Context) error { calls.Add(1); return nil }})

	registry.SetDraining()
	report := registry.Run(context.Background())

	assert.Equal(t, StatusDraining, report.Status)
	assert.False(t, report.Ready())
	assert.Zero(t, calls.Load(), "a draining process must not spend time probing its dependencies")
}

// The checks ping Postgres and Redis, and the probes are polled every few seconds: a ping that started a trace each time
// would fill Tempo with noise.
func TestRegistry_ChecksRunUnderAnUnsampledParentSoTheirPingsAreNotTraced(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("test")

	registry := NewRegistry(time.Second)
	registry.Add(Check{Name: "postgres", Required: true, Fn: func(ctx context.Context) error {
		_, span := tracer.Start(ctx, "ping")
		span.End()
		return nil
	}})

	registry.Run(context.Background())
	assert.Empty(t, exporter.GetSpans(), "the check's span is not recorded")

	_, control := tracer.Start(context.Background(), "ping")
	control.End()
	assert.Len(t, exporter.GetSpans(), 1, "the same call outside the registry is recorded, so the sampler is on")
}

func TestRegistry_ALogLineFromAFailedCheckCarriesTheCallersTrace(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&buf, "json", slog.LevelInfo, "fupi-test"))
	t.Cleanup(func() { slog.SetDefault(previous) })
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))

	registry := NewRegistry(time.Second)
	registry.Add(Check{Name: "redis", Required: false, Fn: down})
	registry.Run(ctx)

	assert.Contains(t, buf.String(), `"msg":"health check failed"`)
	assert.Contains(t, buf.String(), `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`, "the line uses the caller's context, not the unsampled one made for the checks")
}
