package postgres

import (
	"context"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// spanName names the span of a SQL statement. sqlc starts every statement with "-- name: Query :one", which the library
// would turn into the span name "--"; the query name is what a person looking at a trace wants, and it is as low in
// cardinality as the statement set. Any other statement is named after its first word (SELECT, BEGIN, ...), as the library does.
func spanName(statement string) string {
	statement = strings.TrimSpace(statement)
	if rest, found := strings.CutPrefix(statement, "-- name:"); found {
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
		return "query"
	}
	if fields := strings.Fields(statement); len(fields) > 0 && !strings.HasPrefix(fields[0], "--") {
		return strings.ToUpper(fields[0])
	}
	if _, afterComment, found := strings.Cut(statement, "\n"); found && strings.HasPrefix(statement, "--") {
		return spanName(afterComment)
	}
	return "query"
}

// sqlVerb is the first word of the statement (SELECT, INSERT, BEGIN, ...) after any leading comment lines, which is what the
// db.operation.name attribute holds in the semantic conventions.
func sqlVerb(statement string) string {
	statement = strings.TrimSpace(statement)
	for strings.HasPrefix(statement, "--") {
		_, rest, found := strings.Cut(statement, "\n")
		if !found {
			return "query"
		}
		statement = strings.TrimSpace(rest)
	}
	if fields := strings.Fields(statement); len(fields) > 0 {
		return strings.ToUpper(fields[0])
	}
	return "query"
}

// operationTracer corrects db.operation.name. otelpgx fills it from the span name function, which here returns the sqlc
// query name, so it is set again to the SQL verb. Queries sent in a batch keep the query name: otelpgx ends their spans
// before this tracer sees them.
type operationTracer struct {
	*otelpgx.Tracer
}

// TraceQueryStart implements pgx.QueryTracer.
func (t operationTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attribute.String("db.operation.name", sqlVerb(data.SQL)))
	}
	return ctx
}
