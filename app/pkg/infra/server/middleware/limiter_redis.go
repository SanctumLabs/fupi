package middleware

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// redisRateLimiter is a limiter.LimiterHandler deciding requests with redis_rate, so all instances share one bucket per
// key. While Redis is failing, requests are decided by fiber's in-memory fixed window instead.
type redisRateLimiter struct {
	// group is the name of the group this limiter decides for, to label its telemetry.
	group   string
	redis   RateAllower
	timeout time.Duration
	now     func() time.Time
	backend *rateLimitBackend
	metrics *limiterMetrics
}

// New returns the handler for one group. The in-memory fallback is built here, once per group.
func (r *redisRateLimiter) New(cfg limiter.Config) fiber.Handler {
	fallback := limiter.FixedWindow{}.New(cfg)
	limit := redis_rate.Limit{Rate: cfg.Max, Burst: cfg.Max, Period: cfg.Expiration}
	limitHeader := strconv.Itoa(cfg.Max)

	return func(c *fiber.Ctx) error {
		now := r.now()
		key := cfg.KeyGenerator(c)
		if !r.backend.useRedis(now) {
			return r.metrics.inMemory(c, fallback, r.group, key)
		}

		ctx, cancel := context.WithTimeout(c.UserContext(), r.timeout)
		res, err := r.redis.Allow(ctx, key, limit)
		cancel()
		if err != nil {
			r.backend.failed(c.UserContext(), now, err)
			r.metrics.backendError(c.UserContext(), r.group)
			return r.metrics.inMemory(c, fallback, r.group, key)
		}
		r.backend.succeeded(c.UserContext(), now)

		c.Set("X-RateLimit-Limit", limitHeader)
		c.Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
		c.Set("X-RateLimit-Reset", strconv.Itoa(ceilSeconds(res.ResetAfter)))

		limited := res.Allowed == 0
		r.metrics.decided(c, r.group, key, backendRedis, limited, res.Remaining)
		if limited {
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(max(1, ceilSeconds(res.RetryAfter))))
			return cfg.LimitReached(c)
		}
		return c.Next()
	}
}

// rateLimitBackend tracks whether Redis is usable. It is shared by all groups, so one Redis outage moves them all to the
// in-memory fallback.
type rateLimitBackend struct {
	retryAfter time.Duration
	// fallback is true while requests are decided in memory.
	fallback atomic.Bool
	// retryAt is when, in Unix nanoseconds, Redis may be tried again.
	retryAt atomic.Int64
}

func newRateLimitBackend(retryAfter time.Duration) *rateLimitBackend {
	return &rateLimitBackend{retryAfter: retryAfter}
}

// useRedis reports whether a request at now should ask Redis: always while healthy, and after the cooldown otherwise.
func (b *rateLimitBackend) useRedis(now time.Time) bool {
	return !b.fallback.Load() || now.UnixNano() >= b.retryAt.Load()
}

// failed records a Redis error for a request started at now and starts the cooldown, anchored at that start time. Only
// the request switching to the fallback logs.
func (b *rateLimitBackend) failed(ctx context.Context, now time.Time, err error) {
	b.retryAt.Store(now.Add(b.retryAfter).UnixNano())
	if b.fallback.CompareAndSwap(false, true) {
		slog.WarnContext(ctx, "rate limiter falling back to memory", "error", err, "retry_after", b.retryAfter)
	}
}

// succeeded records a Redis answer for a request started at now. A request started before the cooldown ends was in
// flight alongside the failure, so its answer does not end the fallback; a slow Redis would otherwise flap between the
// two. Only the request switching back from the fallback logs.
func (b *rateLimitBackend) succeeded(ctx context.Context, now time.Time) {
	if now.UnixNano() < b.retryAt.Load() {
		return
	}
	if b.fallback.CompareAndSwap(true, false) {
		slog.InfoContext(ctx, "rate limiter back on redis")
	}
}

// active reports whether the in-memory fallback is in use.
func (b *rateLimitBackend) active() bool {
	return b.fallback.Load()
}

func ceilSeconds(d time.Duration) int {
	return int(math.Ceil(d.Seconds()))
}
