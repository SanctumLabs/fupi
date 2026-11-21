package tracing

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
)

type ContextKey string

const (
	TraceIDKey       ContextKey = "x-trace-id"
	RequestIDKey     ContextKey = "x-request-id"
	CorrelationIDKey ContextKey = "x-correlation-id"
)

// GenerateID creates a new unique ID
func GenerateID() string {
	return entity.NewKeyID().String()
}

// NewContext creates a context with all required IDs
func NewContext(ctx context.Context) context.Context {
	// Only generate IDs if they don't exist already
	if GetTraceID(ctx) == "" {
		ctx = context.WithValue(ctx, TraceIDKey, GenerateID())
	}

	// Always generate a new request ID
	ctx = context.WithValue(ctx, RequestIDKey, GenerateID())

	if GetCorrelationID(ctx) == "" {
		ctx = context.WithValue(ctx, CorrelationIDKey, GenerateID())
	}

	return ctx
}

// GetTraceID returns the trace ID from context, if none is available, create one
func GetTraceID(ctx context.Context) string {
	if id, ok := ctx.Value(TraceIDKey).(string); ok {
		return id
	}
	return ""
}

// GetRequestID returns the request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetCorrelationID returns the correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return id
	}
	return ""
}
