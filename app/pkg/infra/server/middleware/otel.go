package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"

// OTelConfig configures OTelMiddleware. The zero value uses the global tracer provider, meter provider and propagator,
// which is what the running API wants; tests inject their own.
type OTelConfig struct {
	// SkipPaths are requests that get no span and no metric (the health probes).
	SkipPaths      []string
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// OTelMiddleware traces and measures every HTTP request: a server span named "METHOD route" (continuing an incoming W3C
// traceparent), stored in the request's user context so everything the handler starts nests under it, plus the
// http.server.request.duration histogram and the http.server.active_requests counter. Put it first in the chain, so the
// middleware behind it see the span. It never records the query string, the client address or request headers.
func OTelMiddleware(cfg OTelConfig) fiber.Handler {
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	if cfg.MeterProvider == nil {
		cfg.MeterProvider = otel.GetMeterProvider()
	}
	if cfg.Propagator == nil {
		cfg.Propagator = otel.GetTextMapPropagator()
	}

	tracer := cfg.TracerProvider.Tracer(instrumentationName)
	meter := cfg.MeterProvider.Meter(instrumentationName)
	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		otel.Handle(err)
	}
	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of HTTP requests being served."),
	)
	if err != nil {
		otel.Handle(err)
	}
	skip := pathSet(cfg.SkipPaths)
	routes := &routeTable{}

	return func(c *fiber.Ctx) error {
		// fasthttp reuses its buffers once the handler returns, and spans and metrics are exported afterwards, so
		// every string taken from the request is copied.
		path := strings.Clone(c.Path())
		if inSet(skip, path) {
			return c.Next()
		}

		start := time.Now()
		method := strings.Clone(c.Method())
		methodAttr := attribute.String("http.request.method", method)
		schemeAttr := attribute.String("url.scheme", requestScheme(c))

		ctx := cfg.Propagator.Extract(c.UserContext(), headerCarrier{&c.Request().Header})
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				methodAttr,
				schemeAttr,
				attribute.String("url.path", path),
				attribute.String("server.address", strings.Clone(c.Hostname())),
				attribute.String("user_agent.original", strings.Clone(c.Get(fiber.HeaderUserAgent))),
			),
		)
		c.SetUserContext(ctx)

		inFlight := metric.WithAttributes(methodAttr, schemeAttr)
		active.Add(ctx, 1, inFlight)

		settle(c, c.Next())

		status := c.Response().StatusCode()
		statusAttr := attribute.Int("http.response.status_code", status)
		spanAttrs := []attribute.KeyValue{statusAttr}
		metricAttrs := []attribute.KeyValue{methodAttr, schemeAttr, statusAttr}
		if route, matched := routes.template(c, path); matched {
			routeAttr := attribute.String("http.route", route)
			spanAttrs = append(spanAttrs, routeAttr)
			metricAttrs = append(metricAttrs, routeAttr)
			span.SetName(method + " " + route)
		}
		span.SetAttributes(spanAttrs...)
		if status >= fiber.StatusInternalServerError {
			span.SetAttributes(attribute.String("error.type", strconv.Itoa(status)))
			span.SetStatus(codes.Error, "")
		}
		span.End()

		active.Add(ctx, -1, inFlight)
		duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(metricAttrs...))
		return nil
	}
}

// requestScheme returns "https" or "http". Fiber's Protocol() returns the raw X-Forwarded-Proto, X-Forwarded-Protocol or
// X-Url-Scheme header the client sent (every client counts as a trusted proxy unless a proxy list is configured), so it
// cannot be a label as it is: anything that is not https counts as http, which bounds the label to two values.
func requestScheme(c *fiber.Ctx) string {
	if strings.EqualFold(c.Protocol(), "https") {
		return "https"
	}
	return "http"
}

// requestHeader is the part of fasthttp's request header the propagator needs.
type requestHeader interface {
	Peek(key string) []byte
	Set(key, value string)
	VisitAll(f func(key, value []byte))
}

// headerCarrier reads incoming headers for the propagator. string(...) copies, so what it returns outlives the request.
type headerCarrier struct{ header requestHeader }

func (c headerCarrier) Get(key string) string { return string(c.header.Peek(key)) }

func (c headerCarrier) Set(key, value string) { c.header.Set(key, value) }

func (c headerCarrier) Keys() []string {
	var keys []string
	c.header.VisitAll(func(key, _ []byte) { keys = append(keys, string(key)) })
	return keys
}
