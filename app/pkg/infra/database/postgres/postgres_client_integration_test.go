//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func spanText(span tracetest.SpanStub) string {
	text := span.Name
	for _, attribute := range span.Attributes {
		text += " " + string(attribute.Key) + "=" + attribute.Value.String()
	}
	for _, event := range span.Events {
		text += " " + event.Name
		for _, attribute := range event.Attributes {
			text += " " + attribute.Value.String()
		}
	}
	return text
}

// A query inside a request is a child span that carries the SQL text; its arguments (which can hold an email address or
// a token) are never recorded.
func TestNewPostgresClient_TracesQueriesWithTheirTextAndNeverTheirArguments(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx) // created after the provider is global, as in main
	t.Cleanup(client.Close)
	exporter.Reset() // the connection and migration spans of the setup

	const secret = "jane.private@example.com"
	requestCtx, request := provider.Tracer("test").Start(ctx, "request")
	var echoed string
	require.NoError(t, client.GetDB().QueryRow(requestCtx, "SELECT $1::text", secret).Scan(&echoed))
	request.End()
	require.Equal(t, secret, echoed)

	var withQueryText, children int
	for _, span := range exporter.GetSpans() {
		assert.NotContains(t, spanText(span), secret, "span %q must not carry the query's arguments", span.Name)
		if span.SpanContext.TraceID() != request.SpanContext().TraceID() || span.Name == "request" {
			continue
		}
		children++
		if strings.Contains(spanText(span), "SELECT $1::text") {
			withQueryText++
		}
	}
	assert.Positive(t, children, "the query runs beneath the request's span")
	assert.Positive(t, withQueryText, "the SQL text is recorded")
}

func TestNewPostgresClient_RecordsPoolStatistics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var names []string
	for _, scope := range rm.ScopeMetrics {
		// The testcontainers Docker client records its own HTTP metrics on the global provider; only otelpgx's count here.
		if !strings.Contains(scope.Scope.Name, "otelpgx") {
			continue
		}
		for _, m := range scope.Metrics {
			names = append(names, m.Name)
		}
	}
	t.Logf("pool metrics: %v", names) // the Dependencies row of the dashboard uses what this prints
	assert.NotEmpty(t, names, "the pool statistics must be registered on the global meter provider")
}

// sqlc statements start with "-- name: Query :one"; their span must carry that name, not "--".
func TestNewPostgresClient_NamesTheSpanAfterTheSqlcQuery(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)
	exporter.Reset()

	requestCtx, request := provider.Tracer("test").Start(ctx, "request")
	var one int
	require.NoError(t, client.GetDB().QueryRow(requestCtx, "-- name: QuerySelectOne :one\nSELECT 1").Scan(&one))
	request.End()

	var names []string
	for _, span := range exporter.GetSpans() {
		names = append(names, span.Name)
	}
	assert.Contains(t, names, "QuerySelectOne")
	assert.NotContains(t, names, "--")
}
