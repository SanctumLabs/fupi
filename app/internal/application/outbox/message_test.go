package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func event(id, destination, key string) ports.OutboxEvent {
	return ports.OutboxEvent{
		ID: id, Destination: destination, PartitionKey: key, EventType: "user.registered",
		Headers:   map[string]string{"event_id": id, "event_type": "user.registered", "aggregate_id": key},
		Payload:   []byte(`{"id":"` + id + `"}`),
		CreatedAt: time.Unix(1_700_000_000, 0),
	}
}

func headerMap(headers []ports.Header) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		out[h.Key] = h.Value
	}
	return out
}

func TestBuildMessage_MapsTheRowOntoATopicKeyValueAndHeaders(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["occurred_at"] = "2026-10-05T10:00:00Z"

	msg := buildMessage(context.Background(), e)

	assert.Equal(t, "identity.events", msg.Topic)
	assert.Equal(t, []byte("user-1"), msg.Key)
	assert.Equal(t, e.Payload, msg.Value, "the payload is forwarded untouched")
	assert.Equal(t, map[string]string{
		"event_id": "e1", "event_type": "user.registered", "aggregate_id": "user-1",
		"occurred_at": "2026-10-05T10:00:00Z", "content-type": "application/json",
	}, headerMap(msg.Headers))
}

func TestBuildMessage_HeadersAreInKeyOrderSoTheOutputIsStable(t *testing.T) {
	msg := buildMessage(context.Background(), event("e1", "identity.events", "user-1"))

	var keys []string
	for _, h := range msg.Headers {
		keys = append(keys, h.Key)
	}
	assert.IsIncreasing(t, keys)
}

func TestBuildMessage_ARowWithoutAPartitionKeyHasANilKey(t *testing.T) {
	msg := buildMessage(context.Background(), event("e1", "identity.events", ""))

	assert.Nil(t, msg.Key, "an empty key must not become a zero-length key: Kafka would hash it to one partition")
}

func publishSpanContext(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
}

func TestBuildMessage_SendsThePublishSpansTraceContextNotTheStoredOne(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	e.Headers["tracestate"] = "vendor=stored"

	msg := buildMessage(publishSpanContext(t), e)

	headers := headerMap(msg.Headers)
	assert.Equal(t, "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", headers["traceparent"])
	assert.NotContains(t, headers, "tracestate", "the stored tracestate belongs to the stored span, not to the publish span")
}

func TestBuildMessage_ForwardsTheStoredTraceContextWhenThereIsNoPublishSpan(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	msg := buildMessage(context.Background(), e)

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", headerMap(msg.Headers)["traceparent"],
		"with tracing off the consumer can still continue the request's trace")
}

func TestBuildMessage_DoesNotChangeTheStoredHeaders(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	buildMessage(publishSpanContext(t), e)

	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", e.Headers["traceparent"])
	assert.NotContains(t, e.Headers, "content-type")
}

func TestStoredTraceContext_ParsesTheWritersTraceparentAndIgnoresGarbage(t *testing.T) {
	e := event("e1", "identity.events", "user-1")
	e.Headers["traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sc := trace.SpanContextFromContext(storedTraceContext(context.Background(), e))
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", sc.TraceID().String())
	assert.Equal(t, "00f067aa0ba902b7", sc.SpanID().String())
	assert.True(t, sc.IsRemote())

	e.Headers["traceparent"] = "not a traceparent"
	assert.False(t, trace.SpanContextFromContext(storedTraceContext(context.Background(), e)).IsValid())

	delete(e.Headers, "traceparent")
	assert.False(t, trace.SpanContextFromContext(storedTraceContext(context.Background(), e)).IsValid())
}
