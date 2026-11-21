package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// DefaultServiceName is the service name of the API when OTEL_SERVICE_NAME is not set.
const DefaultServiceName = "fupi"

// ServiceName is the name this process reports: OTEL_SERVICE_NAME, else service.name in OTEL_RESOURCE_ATTRIBUTES, else
// fallback. That is the order the SDK resolves the resource in, and the log lines and the telemetry resource both use it,
// so a log line and the trace it belongs to name the same service.
func ServiceName(fallback string) string {
	if name := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); name != "" {
		return name
	}
	for _, pair := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		key, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(key) != "service.name" {
			continue
		}
		// The values are percent-encoded, as the SDK reads them.
		if name, err := url.PathUnescape(strings.TrimSpace(value)); err == nil && name != "" {
			return name
		}
	}
	return fallback
}

// NewLogger builds the process logger. The format is "json" (one object per line, the shape the ELK pipeline parses) or
// "text" (for a terminal). Every record carries the service name, and the trace and span IDs of the context passed to the
// log call when that context holds a span, so use the ...Context forms (slog.InfoContext) on the request path. A logger
// derived with WithGroup writes them inside the group, as it does every attribute added to a record: use the root logger
// for lines the log pipeline must correlate. Contexts made by Unsampled for work that has no trace carry no IDs.
func NewLogger(w io.Writer, format string, level slog.Level, service string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(correlationHandler{handler.WithAttrs([]slog.Attr{slog.String("service", service)})})
}

// correlationHandler adds trace_id and span_id to every record whose context carries a span. It wraps the handler it
// returns from WithAttrs and WithGroup, so a logger derived with slog.With keeps adding them. After WithGroup they sit in
// that group, not at the top level.
type correlationHandler struct {
	next slog.Handler
}

func (h correlationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() && !isSynthetic(ctx) {
		record.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
		if sc.HasSpanID() {
			record.AddAttrs(slog.String("span_id", sc.SpanID().String()))
		}
	}
	return h.next.Handle(ctx, record)
}

func (h correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return correlationHandler{h.next.WithAttrs(attrs)}
}

func (h correlationHandler) WithGroup(name string) slog.Handler {
	return correlationHandler{h.next.WithGroup(name)}
}
