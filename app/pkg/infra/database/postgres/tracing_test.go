package postgres

import (
	"context"
	"testing"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/stretchr/testify/assert"
)

// sqlc puts "-- name: Query :one" on the first line of every statement, which the library would turn into the span name
// "--" for every query.
func TestSpanName_UsesTheSqlcQueryName(t *testing.T) {
	cases := map[string]struct {
		statement string
		want      string
	}{
		"sqlc one":      {"-- name: QueryCreateUser :one\nINSERT INTO users (id) VALUES ($1)", "QueryCreateUser"},
		"sqlc many":     {"-- name: QueryOutboxEventsUnSent :many\nSELECT * FROM outbox_events", "QueryOutboxEventsUnSent"},
		"sqlc exec":     {"-- name: QueryDeleteUser :exec\nDELETE FROM users WHERE id = $1", "QueryDeleteUser"},
		"plain select":  {"select 1", "SELECT"},
		"begin":         {"begin", "BEGIN"},
		"leading space": {"  \n  update users set x = 1", "UPDATE"},
		"other comment": {"-- hello\nSELECT 1", "SELECT"},
		"empty":         {"", "query"},
		"name only":     {"-- name:", "query"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, spanName(tc.statement))
		})
	}
}

func TestSqlVerb(t *testing.T) {
	for statement, want := range map[string]string{
		"-- name: QueryCreateUser :one\nINSERT INTO users (id) VALUES ($1)": "INSERT",
		"select 1":                           "SELECT",
		"  begin":                            "BEGIN",
		"-- a\n-- b\nupdate users set x = 1": "UPDATE",
		"-- only a comment":                  "query",
		"":                                   "query",
	} {
		assert.Equal(t, want, sqlVerb(statement), statement)
	}
}

// The span is named after the sqlc query, but db.operation.name is the SQL verb the semantic conventions ask for.
func TestOperationTracer_SetsTheSqlVerbAsTheOperationName(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := operationTracer{otelpgx.NewTracer(otelpgx.WithTracerProvider(provider), otelpgx.WithSpanNameFunc(spanName))}

	ctx, parent := provider.Tracer("test").Start(context.Background(), "request")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "-- name: QueryCreateUser :one\nINSERT INTO users (id) VALUES ($1)"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	parent.End()

	var query tracetest.SpanStub
	for _, span := range exporter.GetSpans() {
		if span.Name != "request" {
			query = span
		}
	}
	assert.Contains(t, query.Name, "QueryCreateUser")
	operations := map[string]string{}
	for _, kv := range query.Attributes {
		operations[string(kv.Key)] = kv.Value.Emit()
	}
	assert.Equal(t, "INSERT", operations["db.operation.name"])
}
