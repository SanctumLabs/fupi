package outbox

import (
	"context"
	"sort"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	headerTraceParent = "traceparent"
	headerTraceState  = "tracestate"
	contentTypeJSON   = "application/json"
)

// mapCarrier lets the W3C propagator read and write the trace headers of a stored event.
type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string { return c[key] }
func (c mapCarrier) Set(key, value string) { c[key] = value }
func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

// storedTraceContext returns ctx carrying the trace context the writer stored with the event, so the publish span is
// a child of the request that wrote it. Without a stored context, or with a malformed one, ctx is returned unchanged.
func storedTraceContext(ctx context.Context, event ports.OutboxEvent) context.Context {
	return propagation.TraceContext{}.Extract(ctx, mapCarrier(event.Headers))
}

// buildMessage turns a claimed event into a broker message: the destination is the topic, the partition key the key, the
// payload the value untouched, and the stored headers are forwarded in key order plus a content type. The trace headers
// are the publish span's, so a consumer's span is a child of it; if there is no valid span (tracing is off) the stored
// ones are forwarded as they are.
func buildMessage(spanCtx context.Context, event ports.OutboxEvent) ports.Message {
	headers := make(map[string]string, len(event.Headers)+1)
	for key, value := range event.Headers {
		headers[key] = value
	}
	if trace.SpanContextFromContext(spanCtx).IsValid() {
		delete(headers, headerTraceParent)
		delete(headers, headerTraceState)
		propagation.TraceContext{}.Inject(spanCtx, mapCarrier(headers))
	}
	headers["content-type"] = contentTypeJSON

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]ports.Header, 0, len(keys))
	for _, key := range keys {
		list = append(list, ports.Header{Key: key, Value: headers[key]})
	}

	var key []byte
	if event.PartitionKey != "" {
		key = []byte(event.PartitionKey)
	}
	return ports.Message{Topic: event.Destination, Key: key, Value: event.Payload, Headers: list}
}
