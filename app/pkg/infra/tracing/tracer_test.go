package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

const w3cTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func withSpan(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(w3cTraceID)
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
}

func TestGetTraceID_PrefersTheW3CTraceIDOfTheSpan(t *testing.T) {
	ctx := context.WithValue(context.Background(), TraceIDKey, "legacy-id")

	assert.Equal(t, w3cTraceID, GetTraceID(withSpan(t, ctx)))
}

func TestGetTraceID_FallsBackToTheStoredIDWithoutASpan(t *testing.T) {
	ctx := context.WithValue(context.Background(), TraceIDKey, "legacy-id")

	assert.Equal(t, "legacy-id", GetTraceID(ctx))
}

func TestGetTraceID_IsEmptyWithNeitherASpanNorAStoredID(t *testing.T) {
	assert.Empty(t, GetTraceID(context.Background()))
}

// NewContext only generates a trace ID when there is none, so inside a span it must not shadow the W3C ID.
func TestNewContext_KeepsTheSpansTraceIDAndStillAddsTheOtherIDs(t *testing.T) {
	ctx := NewContext(withSpan(t, context.Background()))

	assert.Equal(t, w3cTraceID, GetTraceID(ctx))
	assert.NotEmpty(t, GetRequestID(ctx))
	assert.NotEmpty(t, GetCorrelationID(ctx))
}

func TestNewContext_GeneratesATraceIDWithoutASpan(t *testing.T) {
	ctx := NewContext(context.Background())

	assert.NotEmpty(t, GetTraceID(ctx))
	assert.NotEqual(t, w3cTraceID, GetTraceID(ctx))
}
