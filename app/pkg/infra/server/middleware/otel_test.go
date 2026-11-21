package middleware

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var probePaths = []string{"/health", "/health/ready"}

// fixture is a Fiber app with OTelMiddleware in front of a few routes, wired to an in-memory span exporter and a manual
// metric reader so a test can read back exactly what would have been exported.
type fixture struct {
	app     *fiber.App
	spans   *tracetest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	spans := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spans)))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})

	f := &fixture{app: fiber.New(), spans: spans, metrics: reader}
	f.app.Use(OTelMiddleware(OTelConfig{
		SkipPaths:      probePaths,
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
		Propagator:     propagation.TraceContext{},
	}))
	f.app.Use(fiberrecover.New())
	f.app.Get("/", func(c *fiber.Ctx) error { return c.SendString("root") })
	f.app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("user") })
	f.app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	f.app.Get("/health/ready", func(c *fiber.Ctx) error { return c.SendString("ok") })
	f.app.Get("/conflict", func(c *fiber.Ctx) error { return fiber.NewError(fiber.StatusConflict, "taken") })
	f.app.Get("/boom", func(c *fiber.Ctx) error { return errors.New("boom") })
	f.app.Get("/panic", func(c *fiber.Ctx) error { panic("kaboom") })
	return f
}

func (f *fixture) do(t *testing.T, method, target string, headers ...string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	return resp
}

func (f *fixture) onlySpan(t *testing.T) tracetest.SpanStub {
	t.Helper()
	spans := f.spans.GetSpans()
	require.Len(t, spans, 1)
	return spans[0]
}

func attrs(kvs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(kvs))
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value
	}
	return out
}

func (f *fixture) collect(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, f.metrics.Collect(context.Background(), &rm))
	out := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func (f *fixture) durationPoints(t *testing.T) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	m, ok := f.collect(t)["http.server.request.duration"]
	if !ok {
		return nil
	}
	histogram, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "the duration must be a float64 histogram, got %T", m.Data)
	return histogram.DataPoints
}

func TestOTelMiddleware_NamesTheSpanAfterTheRouteTemplate(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/users/42", "User-Agent", "curl/8")
	assert.Equal(t, 200, resp.StatusCode)

	span := f.onlySpan(t)
	assert.Equal(t, "GET /users/:id", span.Name, "the template, never the id, so span names stay low-cardinality")
	assert.Equal(t, trace.SpanKindServer, span.SpanKind)
	assert.Equal(t, codes.Unset, span.Status.Code)

	got := attrs(span.Attributes)
	assert.Equal(t, "GET", got["http.request.method"].AsString())
	assert.Equal(t, "/users/:id", got["http.route"].AsString())
	assert.Equal(t, int64(200), got["http.response.status_code"].AsInt64())
	assert.Equal(t, "/users/42", got["url.path"].AsString())
	assert.Equal(t, "http", got["url.scheme"].AsString())
	assert.Equal(t, "example.com", got["server.address"].AsString())
	assert.Equal(t, "curl/8", got["user_agent.original"].AsString())
}

// Fiber reports its catch-all "/" for a path that matched no route, so "/" is only a route when the path is "/".
func TestOTelMiddleware_TellsTheRootRouteFromAnUnmatchedPath(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/")
	root := f.onlySpan(t)
	assert.Equal(t, "GET /", root.Name)
	assert.Equal(t, "/", attrs(root.Attributes)["http.route"].AsString())
	f.spans.Reset()

	resp := f.do(t, "GET", "/no/such/path/123")
	assert.Equal(t, 404, resp.StatusCode)
	missing := f.onlySpan(t)
	assert.Equal(t, "GET", missing.Name, "an unmatched route is named after the method only")
	_, hasRoute := attrs(missing.Attributes)["http.route"]
	assert.False(t, hasRoute, "the raw path must never become the route")
	assert.Equal(t, codes.Unset, missing.Status.Code, "a 404 is not a server error")
	assert.Equal(t, "/no/such/path/123", attrs(missing.Attributes)["url.path"].AsString())
}

func TestOTelMiddleware_AWrongMethodIsAnUnmatchedRouteToo(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "DELETE", "/users/42")

	assert.Equal(t, 405, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, "DELETE", span.Name)
	assert.Equal(t, int64(405), attrs(span.Attributes)["http.response.status_code"].AsInt64())
	_, hasRoute := attrs(span.Attributes)["http.route"]
	assert.False(t, hasRoute)
}

// A handler that returns an error has not written its response yet; the status the span records must be the final one.
func TestOTelMiddleware_RecordsTheStatusOfAReturnedError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/conflict")

	assert.Equal(t, 409, resp.StatusCode, "the client still gets the error handler's answer")
	span := f.onlySpan(t)
	assert.Equal(t, int64(409), attrs(span.Attributes)["http.response.status_code"].AsInt64())
	assert.Equal(t, codes.Unset, span.Status.Code, "a 4xx is the client's error, not the server span's")
}

func TestOTelMiddleware_AFiveHundredMarksTheSpanAsAnError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/boom")

	assert.Equal(t, 500, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Equal(t, "500", attrs(span.Attributes)["error.type"].AsString())
	assert.Equal(t, "GET /boom", span.Name)
}

func TestOTelMiddleware_APanicBecomesAFiveHundredSpanError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/panic")

	assert.Equal(t, 500, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Equal(t, int64(500), attrs(span.Attributes)["http.response.status_code"].AsInt64())
}

func TestOTelMiddleware_ProbesGetNoSpanAndNoMetric(t *testing.T) {
	f := newFixture(t)

	for _, target := range []string{"/health", "/health/", "/Health", "/health/ready", "/HEALTH/READY/"} {
		resp := f.do(t, "GET", target)
		assert.Equal(t, 200, resp.StatusCode, target+" must still be served")
	}

	assert.Empty(t, f.spans.GetSpans())
	assert.Empty(t, f.durationPoints(t))
}

func TestOTelMiddleware_ContinuesAnIncomingTraceparent(t *testing.T) {
	f := newFixture(t)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentSpanID = "00f067aa0ba902b7"

	var inHandler trace.SpanContext
	f.app.Get("/trace", func(c *fiber.Ctx) error {
		inHandler = trace.SpanContextFromContext(c.UserContext())
		return c.SendStatus(200)
	})

	f.do(t, "GET", "/trace", "traceparent", "00-"+traceID+"-"+parentSpanID+"-01")

	span := f.onlySpan(t)
	assert.Equal(t, traceID, span.SpanContext.TraceID().String(), "the request joins the caller's trace")
	assert.Equal(t, parentSpanID, span.Parent.SpanID().String())
	assert.True(t, span.Parent.IsRemote())
	assert.Equal(t, span.SpanContext.SpanID(), inHandler.SpanID(), "the handler sees the server span, so what it starts nests under it")
	assert.Equal(t, traceID, inHandler.TraceID().String())
}

func TestOTelMiddleware_StartsARootTraceWithoutATraceparent(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/1")

	span := f.onlySpan(t)
	assert.False(t, span.Parent.IsValid())
	assert.True(t, span.SpanContext.TraceID().IsValid())
}

func TestOTelMiddleware_RecordsTheRequestDurationHistogram(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/42")

	m := f.collect(t)["http.server.request.duration"]
	assert.Equal(t, "s", m.Unit, "the unit is what makes the collector name it ..._seconds")
	points := f.durationPoints(t)
	require.Len(t, points, 1)
	assert.Equal(t, uint64(1), points[0].Count)
	assert.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}, points[0].Bounds)

	got := points[0].Attributes
	method, _ := got.Value("http.request.method")
	route, _ := got.Value("http.route")
	status, _ := got.Value("http.response.status_code")
	scheme, _ := got.Value("url.scheme")
	assert.Equal(t, "GET", method.AsString())
	assert.Equal(t, "/users/:id", route.AsString())
	assert.Equal(t, int64(200), status.AsInt64())
	assert.Equal(t, "http", scheme.AsString())
	assert.Equal(t, 4, got.Len(), "exactly the four labels the dashboard and the alert use, nothing high-cardinality")
}

func TestOTelMiddleware_AnUnmatchedRouteHasNoRouteLabel(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/no/such/path/123")

	points := f.durationPoints(t)
	require.Len(t, points, 1)
	_, hasRoute := points[0].Attributes.Value("http.route")
	assert.False(t, hasRoute)
	status, _ := points[0].Attributes.Value("http.response.status_code")
	assert.Equal(t, int64(404), status.AsInt64())
}

func TestOTelMiddleware_CountsRequestsInFlight(t *testing.T) {
	f := newFixture(t)

	var duringRequest int64
	f.app.Get("/slow", func(c *fiber.Ctx) error {
		duringRequest = f.activeRequests(t)
		return c.SendStatus(200)
	})

	f.do(t, "GET", "/slow")

	assert.Equal(t, int64(1), duringRequest, "the request being served is counted")
	assert.Equal(t, int64(0), f.activeRequests(t), "and is no longer counted once it is done")
}

func (f *fixture) activeRequests(t *testing.T) int64 {
	t.Helper()
	m, ok := f.collect(t)["http.server.active_requests"]
	require.True(t, ok)
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "got %T", m.Data)
	var total int64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	return total
}

// Privacy: the query string and the headers can hold tokens and personal data, and none of it is recorded.
func TestOTelMiddleware_NeverRecordsTheQueryStringOrHeaders(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/42?token=query-secret&email=jane@example.com",
		"Authorization", "Bearer header-secret", "Cookie", "session=cookie-secret", "X-Forwarded-For", "203.0.113.9")

	span := f.onlySpan(t)
	for key, value := range attrs(span.Attributes) {
		assert.NotContains(t, value.String(), "query-secret", key)
		assert.NotContains(t, value.String(), "jane@example.com", key)
		assert.NotContains(t, value.String(), "header-secret", key)
		assert.NotContains(t, value.String(), "cookie-secret", key)
		assert.NotContains(t, value.String(), "203.0.113.9", key)
		assert.False(t, strings.HasPrefix(key, "http.request.header"), key)
		assert.NotEqual(t, "client.address", key)
	}
	assert.Equal(t, "/users/42", attrs(span.Attributes)["url.path"].AsString())
}

// fasthttp reuses a connection's buffers for the next request, and a span is exported after its request is over: an
// attribute that still pointed into those buffers would show the next request's path.
func TestOTelMiddleware_AttributesSurviveTheNextRequestOnTheSameConnection(t *testing.T) {
	f := newFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = f.app.Listener(ln) }()
	t.Cleanup(func() { _ = f.app.Shutdown() })
	client := &http.Client{}
	base := "http://" + ln.Addr().String()

	for _, target := range []string{"/first-path-aaaaaaaa", "/second-path-bbbbbbb"} {
		resp, err := client.Get(base + target)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	spans := f.spans.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, "/first-path-aaaaaaaa", attrs(spans[0].Attributes)["url.path"].AsString())
	assert.Equal(t, "/second-path-bbbbbbb", attrs(spans[1].Attributes)["url.path"].AsString())
}

// Fiber's Protocol() returns the raw X-Forwarded-Proto, X-Forwarded-Protocol or X-Url-Scheme header of the request when no
// trusted proxy list is configured, so using it as a label lets any client create series without bound (the SDK then caps
// the instrument and every later route and status lands in the overflow series).
func TestOTelMiddleware_TheSchemeIsHttpOrHttpsWhateverTheClientSends(t *testing.T) {
	f := newFixture(t)

	for _, header := range [][2]string{
		{"X-Forwarded-Proto", "evil-0"}, {"X-Forwarded-Proto", "evil-1"}, {"X-Forwarded-Protocol", "evil-2"},
		{"X-Url-Scheme", "anything-goes"}, {"X-Forwarded-Proto", "HTTPS"}, {"X-Forwarded-Proto", "https"},
	} {
		f.do(t, "GET", "/users/42", header[0], header[1])
	}

	schemes := map[string]bool{}
	for _, point := range f.durationPoints(t) {
		scheme, _ := point.Attributes.Value("url.scheme")
		schemes[scheme.AsString()] = true
	}
	assert.Equal(t, map[string]bool{"http": true, "https": true}, schemes, "the histogram's url.scheme label has two possible values")

	for _, span := range f.spans.GetSpans() {
		assert.Contains(t, []string{"http", "https"}, attrs(span.Attributes)["url.scheme"].AsString())
	}
}
