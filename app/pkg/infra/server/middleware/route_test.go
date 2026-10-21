package middleware

import (
	"bufio"
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// routeOf runs one request through OTelMiddleware and returns the http.route of its span ("" when it has none).
func routeOf(t *testing.T, app *fiber.App, spans *tracetest.InMemoryExporter, method, target string) (route string, status int) {
	t.Helper()
	spans.Reset()
	resp, err := app.Test(httptest.NewRequest(method, target, nil))
	require.NoError(t, err)
	got := spans.GetSpans()
	require.Len(t, got, 1)
	return attrs(got[0].Attributes)["http.route"].AsString(), resp.StatusCode
}

func tracedApp(t *testing.T) (*fiber.App, *tracetest.InMemoryExporter) {
	t.Helper()
	spans := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spans)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	app := fiber.New()
	app.Use(OTelMiddleware(OTelConfig{TracerProvider: provider, Propagator: propagation.TraceContext{}}))
	return app, spans
}

// With no "/" route registered, Fiber still reports the "/" of the middleware chain for a request to "/".
func TestRouteTable_ARequestToRootWithNoRootRouteHasNoRoute(t *testing.T) {
	app, spans := tracedApp(t)
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("user") })

	route, status := routeOf(t, app, spans, "GET", "/")

	assert.Equal(t, 404, status)
	assert.Empty(t, route)
}

// A middleware that answers (401 from the auth middleware, a CORS preflight, an idempotency replay) stops the chain before
// the handler, but the request still belongs to the route it was aimed at.
func TestRouteTable_ARequestAnsweredByAMiddlewareKeepsItsRoute(t *testing.T) {
	app, spans := tracedApp(t)
	app.Use(func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusUnauthorized) })
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("user") })
	app.Post("/urls", func(c *fiber.Ctx) error { return c.SendString("created") })

	route, status := routeOf(t, app, spans, "GET", "/users/42")
	assert.Equal(t, 401, status)
	assert.Equal(t, "/users/:id", route, "the id must not leak into the label")

	route, _ = routeOf(t, app, spans, "POST", "/urls")
	assert.Equal(t, "/urls", route)

	route, status = routeOf(t, app, spans, "GET", "/no/such/path")
	assert.Equal(t, 401, status)
	assert.Empty(t, route, "no registered route matches, so the raw path is not a label")

	route, _ = routeOf(t, app, spans, "DELETE", "/users/42")
	assert.Empty(t, route, "the route is only registered for GET")
}

// Fiber trims every trailing slash when it routes, so /health// is the same path as /health.
func TestInSet_IgnoresCaseAndAnyNumberOfTrailingSlashes(t *testing.T) {
	set := pathSet([]string{"/health", "/"})

	for _, path := range []string{"/health", "/HEALTH", "/health/", "/health//", "/health///", "/"} {
		assert.True(t, inSet(set, path), path)
	}
	for _, path := range []string{"/healthy", "/health/x", "/other", "//other"} {
		assert.False(t, inSet(set, path), path)
	}
}

// A streamed body is not in memory, so the access log must neither buffer it to count it nor report a length it does not
// have.
func TestAccessLog_DoesNotBufferAStreamedBody(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)
	app.Get("/stream", func(c *fiber.Ctx) error {
		return c.SendStream(bufio.NewReader(strings.NewReader("streamed")))
	})

	_, err := app.Test(httptest.NewRequest("GET", "/stream", nil))
	require.NoError(t, err)
	_, err = app.Test(httptest.NewRequest("GET", "/users/1", nil))
	require.NoError(t, err)

	lines := logLines(t, buf)
	require.Len(t, lines, 2)
	assert.EqualValues(t, -1, lines[0]["bytes"])
	assert.EqualValues(t, 5, lines[1]["bytes"])
}
