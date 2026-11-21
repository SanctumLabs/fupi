package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// logTo installs a JSON default logger at the given level that writes to the returned buffer until the test ends.
func logTo(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&buf, "json", level, "fupi-test"))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &line), raw)
		lines = append(lines, line)
	}
	return lines
}

// accessLogApp is the chain the server uses: tracing first, then the request ID, then the access log.
func accessLogApp(t *testing.T) *fiber.App {
	t.Helper()
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	app := fiber.New()
	app.Use(OTelMiddleware(OTelConfig{SkipPaths: probePaths, TracerProvider: provider, Propagator: propagation.TraceContext{}}))
	app.Use(RequestIdMiddleware())
	app.Use(AccessLogMiddleware(probePaths))
	app.Use(fiberrecover.New())
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("hello") })
	app.Get("/conflict", func(c *fiber.Ctx) error { return fiber.NewError(fiber.StatusConflict, "taken") })
	app.Get("/panic", func(c *fiber.Ctx) error { panic("kaboom") })
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func TestAccessLog_WritesOneLineWithTheRequestsFacts(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	req := httptest.NewRequest("GET", "/users/42?token=secret", nil)
	req.Header.Set("X-Request-ID", "req-123")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	line := lines[0]
	assert.Equal(t, "request", line["msg"])
	assert.Equal(t, "GET", line["method"])
	assert.Equal(t, "/users/:id", line["route"])
	assert.Equal(t, "/users/42", line["path"], "the query string is not logged")
	assert.EqualValues(t, 200, line["status"])
	assert.EqualValues(t, 5, line["bytes"])
	assert.Equal(t, "req-123", line["request_id"])
	assert.GreaterOrEqual(t, line["duration_ms"], float64(0))
	assert.Equal(t, "fupi-test", line["service"])
	assert.NotContains(t, buf.String(), "secret")
}

func TestAccessLog_CarriesTheTraceAndSpanIDsOfTheRequest(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)
	var spanContext trace.SpanContext
	app.Get("/trace", func(c *fiber.Ctx) error {
		spanContext = trace.SpanContextFromContext(c.UserContext())
		return c.SendStatus(200)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/trace", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	require.True(t, spanContext.IsValid())
	assert.Equal(t, spanContext.TraceID().String(), lines[0]["trace_id"])
	assert.Equal(t, spanContext.SpanID().String(), lines[0]["span_id"])
}

func TestAccessLog_LogsTheFinalStatusOfAReturnedErrorAndOfAPanic(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	for _, target := range []string{"/conflict", "/panic"} {
		_, err := app.Test(httptest.NewRequest("GET", target, nil))
		require.NoError(t, err)
	}

	lines := logLines(t, buf)
	require.Len(t, lines, 2)
	assert.EqualValues(t, 409, lines[0]["status"])
	assert.EqualValues(t, 500, lines[1]["status"])
}

func TestAccessLog_AnUnmatchedPathHasNoRoute(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	_, err := app.Test(httptest.NewRequest("GET", "/no/such/path", nil))
	require.NoError(t, err)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	assert.Equal(t, "", lines[0]["route"])
	assert.Equal(t, "/no/such/path", lines[0]["path"])
	assert.EqualValues(t, 404, lines[0]["status"])
}

func TestAccessLog_ProbesAreLoggedAtDebugOnly(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	_, err := app.Test(httptest.NewRequest("GET", "/health", nil))
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "at info level a probe leaves no line")

	debug := logTo(t, slog.LevelDebug)
	_, err = app.Test(httptest.NewRequest("GET", "/health", nil))
	require.NoError(t, err)
	lines := logLines(t, debug)
	require.Len(t, lines, 1)
	assert.Equal(t, "DEBUG", lines[0]["level"])
	assert.Equal(t, "/health", lines[0]["path"])
}
