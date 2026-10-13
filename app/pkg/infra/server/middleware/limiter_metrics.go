package middleware

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const limiterInstrumentationName = "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"

const (
	backendRedis  = "redis"
	backendMemory = "memory"
)

// limitedKey is the Locals key under which LimitReached flags a request as limited, for the in-memory limiter, which
// does not report its decision otherwise.
type limitedKey struct{}

// limiterMetrics are the instruments of the rate limiter. Attributes never include the client key: an IP or user ID
// would make the cardinality unbounded. The key goes on the span only.
type limiterMetrics struct {
	requests      metric.Int64Counter
	backendErrors metric.Int64Counter
}

// newLimiterMetrics creates the instruments. backend is nil when there is no Redis, in which case the fallback gauge
// reads 0: with no Redis, no fallback is active.
func newLimiterMetrics(provider metric.MeterProvider, backend *rateLimitBackend) (*limiterMetrics, error) {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	meter := provider.Meter(limiterInstrumentationName)
	m := &limiterMetrics{}

	var errs []error
	var err error
	m.requests, err = meter.Int64Counter("http.ratelimit.requests",
		metric.WithDescription("Requests decided by the rate limiter, by group, outcome and backend."))
	errs = append(errs, err)
	m.backendErrors, err = meter.Int64Counter("http.ratelimit.backend.errors",
		metric.WithDescription("Redis errors that sent a request to the in-memory limiter."))
	errs = append(errs, err)
	_, err = meter.Int64ObservableGauge("http.ratelimit.fallback.active",
		metric.WithDescription("1 while rate limiting falls back to memory because Redis fails."),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			var active int64
			if backend != nil && backend.active() {
				active = 1
			}
			observer.Observe(active)
			return nil
		}))
	errs = append(errs, err)

	return m, errors.Join(errs...)
}

// decided records the decision on a request: a count, and the attributes of the span in the request context. key is the
// full limiter key, e.g. "auth:ip:203.0.113.7". A nil limiterMetrics records nothing.
func (m *limiterMetrics) decided(c *fiber.Ctx, group, key, backend string, limited bool, remaining int) {
	if m == nil {
		return
	}

	outcome := "allowed"
	if limited {
		outcome = "limited"
	}
	m.requests.Add(c.UserContext(), 1, metric.WithAttributes(
		attribute.String("group", group),
		attribute.String("outcome", outcome),
		attribute.String("backend", backend),
	))

	trace.SpanFromContext(c.UserContext()).SetAttributes(
		attribute.String("ratelimit.group", group),
		attribute.String("ratelimit.key", key),
		attribute.String("ratelimit.backend", backend),
		attribute.Bool("ratelimit.limited", limited),
		attribute.Int("ratelimit.remaining", remaining),
	)
}

// backendError counts a Redis error for group. A nil limiterMetrics records nothing.
func (m *limiterMetrics) backendError(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.backendErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("group", group)))
}

// inMemory runs handler, a fiber fixed window, and records its decision. The limit is read from the LimitReached flag
// and the remaining count from the response header the window sets.
func (m *limiterMetrics) inMemory(c *fiber.Ctx, handler fiber.Handler, group, key string) error {
	err := handler(c)
	limited, _ := c.Locals(limitedKey{}).(bool)
	remaining, _ := strconv.Atoi(c.GetRespHeader("X-RateLimit-Remaining"))
	m.decided(c, group, key, backendMemory, limited, remaining)
	return err
}
