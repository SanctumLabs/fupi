package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

// spanContext returns a context that carries a span, sampled or not.
func spanContext(t *testing.T, flags trace.TraceFlags) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(testTraceID)
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex(testSpanID)
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: flags,
	}))
}

func logLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), buf.String())
	return line
}

func TestNewLogger_JSONCarriesTheKeysTheELKPipelineMaps(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, trace.FlagsSampled), "hello", "user", "u-1")

	line := logLine(t, &buf)
	assert.Equal(t, "hello", line["msg"])
	assert.Equal(t, "INFO", line["level"])
	assert.NotEmpty(t, line["time"])
	assert.Equal(t, "fupi", line["service"])
	assert.Equal(t, "u-1", line["user"])
	assert.Equal(t, testTraceID, line["trace_id"])
	assert.Equal(t, testSpanID, line["span_id"])
}

func TestNewLogger_OmitsTheIDsWithoutASpan(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(context.Background(), "no span")
	logger.Info("no context at all")

	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		assert.NotContains(t, raw, "trace_id")
		assert.NotContains(t, raw, "span_id")
		assert.Contains(t, raw, `"service":"fupi"`)
	}
}

// A span that was sampled out still has IDs, and the log line is still worth finding by them.
func TestNewLogger_AddsTheIDsOfAnUnsampledSpanToo(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, 0), "sampled out")

	assert.Equal(t, testTraceID, logLine(t, &buf)["trace_id"])
}

// slog.With wraps the handler through WithAttrs; a wrapper that forgot to wrap its result would silently stop adding IDs.
func TestNewLogger_DerivedLoggersKeepAddingTheIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.With("component", "x").WithGroup("g").InfoContext(spanContext(t, trace.FlagsSampled), "derived", "k", "v")

	line := logLine(t, &buf)
	assert.Equal(t, "x", line["component"])
	group, ok := line["g"].(map[string]any)
	require.True(t, ok, "WithGroup must still group the call's attributes")
	assert.Equal(t, "v", group["k"])
	// Handle runs inside the open group, so the IDs land in it; what matters is that they are not lost.
	assert.Equal(t, testTraceID, group["trace_id"])
}

func TestNewLogger_HonoursTheLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelWarn, "fupi")

	logger.Info("dropped")
	logger.Debug("dropped")
	assert.Empty(t, buf.String())

	logger.Warn("kept")
	assert.Contains(t, buf.String(), "kept")
}

func TestNewLogger_TextFormatIsForTerminals(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, trace.FlagsSampled), "hello")

	out := buf.String()
	assert.False(t, strings.HasPrefix(out, "{"), "text format must not be JSON: %s", out)
	assert.Contains(t, out, "msg=hello")
	assert.Contains(t, out, "service=fupi")
	assert.Contains(t, out, "trace_id="+testTraceID)
}

func TestServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	assert.Equal(t, "fupi", ServiceName(DefaultServiceName), "an empty value counts as unset")
	assert.Equal(t, "fupi-worker", ServiceName("fupi-worker"))

	t.Setenv("OTEL_SERVICE_NAME", "  fupi-api ")
	assert.Equal(t, "fupi-api", ServiceName("fupi-worker"), "the variable wins over the fallback")
}

func TestServiceName_ReadsServiceNameFromTheResourceAttributes(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")

	for attrs, want := range map[string]string{
		"service.name=fupi-api": "fupi-api",
		"deployment.environment.name=prod, service.name = fupi%20api": "fupi api",
		"custom.service.name=other":                                   "fupi-worker",
		"service.name=":                                               "fupi-worker",
		"":                                                            "fupi-worker",
	} {
		t.Setenv("OTEL_RESOURCE_ATTRIBUTES", attrs)
		assert.Equal(t, want, ServiceName("fupi-worker"), attrs)
	}

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=from-attributes")
	t.Setenv("OTEL_SERVICE_NAME", "from-variable")
	assert.Equal(t, "from-variable", ServiceName("fupi-worker"), "OTEL_SERVICE_NAME wins, as it does in the resource")
}

// The IDs Unsampled makes up belong to no trace, so a line carrying them would point at nothing.
func TestNewLogger_LeavesOutTheSyntheticIDsOfUnsampled(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", slog.LevelInfo, "fupi")

	logger.InfoContext(Unsampled(context.Background()), "readiness check")

	assert.NotContains(t, buf.String(), "trace_id")
	assert.NotContains(t, buf.String(), "span_id")
}

func TestNewLogger_KeepsTheIDsOfARealSpanUnderUnsampled(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", slog.LevelInfo, "fupi")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.InfoContext(Unsampled(ctx), "readiness check")

	assert.Contains(t, buf.String(), "trace_id="+sc.TraceID().String())
}

// Under WithGroup every attribute of the record, these two included, is written inside the group.
func TestNewLogger_UnderWithGroupTheIDsAreInsideTheGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", slog.LevelInfo, "fupi").WithGroup("request")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})

	logger.InfoContext(trace.ContextWithSpanContext(context.Background(), sc), "hello")

	assert.Contains(t, buf.String(), "request.trace_id="+sc.TraceID().String())
}
