package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// instrumented builds the real server (its real middleware chain) with the global tracer provider and propagator
// pointed at an in-memory exporter, and the default logger at a JSON buffer, until the test ends.
func instrumented(t *testing.T, routes ...router.Route) (*Server, *tracetest.InMemoryExporter, *bytes.Buffer) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&logs, "json", slog.LevelInfo, "fupi-test"))

	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		otel.SetTextMapPropagator(previousPropagator)
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	srv := NewServer(ServerConfig{AppName: "fupi-test", ProbePaths: []string{"/health", "/health/ready"}})
	srv.RegisterHandlers([]router.Router{stubRouter{routes: routes}})
	return srv, exporter, &logs
}

func TestNewServer_TracesARequestAndLogsItWithTheSameTraceID(t *testing.T) {
	srv, exporter, logs := instrumented(t,
		router.NewGetRoute("/users/:id", func(c *fiber.Ctx) error { return c.SendString("hi") }),
	)

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/users/42", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /users/:id", spans[0].Name)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &line), logs.String())
	assert.Equal(t, "request", line["msg"])
	assert.Equal(t, spans[0].SpanContext.TraceID().String(), line["trace_id"], "the access log line and the span share one trace ID")
	assert.NotEmpty(t, line["request_id"], "the request ID middleware runs before the access log")
}

func TestNewServer_AHandlerPanicIsAFiveHundredSpanError(t *testing.T) {
	srv, exporter, _ := instrumented(t,
		router.NewGetRoute("/panic", func(c *fiber.Ctx) error { panic("kaboom") }),
	)

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/panic", nil))
	require.NoError(t, err)

	assert.Equal(t, 500, resp.StatusCode)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestNewServer_ProbesAreNeitherTracedNorLoggedAtInfo(t *testing.T) {
	srv, exporter, logs := instrumented(t,
		router.NewGetRoute("/health", func(c *fiber.Ctx) error { return c.SendString("ok") }),
		router.NewGetRoute("/health/ready", func(c *fiber.Ctx) error { return c.SendString("ok") }),
	)

	for _, target := range []string{"/health", "/health/ready"} {
		resp, err := srv.App().Test(httptest.NewRequest("GET", target, nil))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
	}

	assert.Empty(t, exporter.GetSpans())
	assert.Empty(t, logs.String())
}

// The Fiber monitor page was unauthenticated, exposed runtime statistics and was not Prometheus. Metrics leave over OTLP.
func TestNewServer_DoesNotServeAMetricsPage(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/metrics", nil))
	require.NoError(t, err)

	assert.Equal(t, 404, resp.StatusCode)
}
