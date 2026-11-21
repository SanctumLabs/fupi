package logger

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type TracingLogger struct {
	*zap.SugaredLogger
}

func NewTracingLogger(logger *zap.Logger) *TracingLogger {
	return &TracingLogger{
		SugaredLogger: logger.Sugar(),
	}
}

func (l *TracingLogger) WithContext(ctx context.Context) *zap.SugaredLogger {
	span := trace.SpanFromContext(ctx)

	// Extract trace and span IDs
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	lg := l.With(
		"x-trace-id", traceID,
		"span_id", spanID,
	)
	return lg
}
