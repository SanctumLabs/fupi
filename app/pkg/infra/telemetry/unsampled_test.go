package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// recordingProvider is a tracer provider with the SDK's default (parent-based, always-on) sampler.
func recordingProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, exporter
}

func TestUnsampled_SpansBeneathItAreNotRecorded(t *testing.T) {
	provider, exporter := recordingProvider(t)
	tracer := provider.Tracer("test")

	_, control := tracer.Start(context.Background(), "control")
	control.End()
	require.Len(t, exporter.GetSpans(), 1, "a root span is recorded, so the sampler really is on")
	exporter.Reset()

	ctx, probe := tracer.Start(Unsampled(context.Background()), "pg ping")
	assert.False(t, probe.IsRecording())
	assert.True(t, trace.SpanContextFromContext(ctx).IsValid(), "the child still has valid IDs, so nothing under it breaks")
	probe.End()

	assert.Empty(t, exporter.GetSpans())
}

func TestUnsampled_KeepsTheIDsOfAnExistingSpanButStopsRecording(t *testing.T) {
	provider, exporter := recordingProvider(t)
	tracer := provider.Tracer("test")

	parentCtx, parent := tracer.Start(context.Background(), "request")
	defer parent.End()

	ctx := Unsampled(parentCtx)
	childCtx, child := tracer.Start(ctx, "ping")
	child.End()

	assert.Equal(t, parent.SpanContext().TraceID(), trace.SpanContextFromContext(ctx).TraceID())
	assert.False(t, trace.SpanContextFromContext(ctx).IsSampled())
	assert.Equal(t, parent.SpanContext().TraceID(), trace.SpanContextFromContext(childCtx).TraceID())
	assert.Empty(t, exporter.GetSpans(), "the child of an unsampled parent is not recorded")
	assert.True(t, trace.SpanContextFromContext(parentCtx).IsSampled(), "the caller's own context is not changed")
}

func TestUnsampled_GivesEachCallItsOwnIDs(t *testing.T) {
	first := trace.SpanContextFromContext(Unsampled(context.Background()))
	second := trace.SpanContextFromContext(Unsampled(context.Background()))

	assert.True(t, first.IsValid())
	assert.NotEqual(t, first.TraceID(), second.TraceID())
}
