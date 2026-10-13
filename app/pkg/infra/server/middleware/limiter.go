package middleware

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"go.opentelemetry.io/otel/metric"
)

// LimitedMessage is the message of the 429 response body sent to a client that exceeded its limit.
const LimitedMessage = "too many requests"

// RateLimitRule is the number of requests allowed per window.
type RateLimitRule struct {
	Max    int
	Window time.Duration
}

// RateLimitGroup is a set of requests that share a rule and a bucket per client.
type RateLimitGroup struct {
	// Name identifies the group and prefixes its limiter keys, so groups never share buckets.
	Name string
	Rule RateLimitRule
	// Match reports whether the request belongs to this group. A nil Match matches every request.
	Match func(*fiber.Ctx) bool
	// Key returns the client identity to limit, e.g. "user:<id>". A nil Key, or an empty result, falls back to the
	// client IP.
	Key func(*fiber.Ctx) string
}

// RateAllower decides whether a request under a key is within a limit. *redis_rate.Limiter satisfies it.
type RateAllower interface {
	Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error)
}

// RateLimiterConfig configures RateLimiterMiddleware.
type RateLimiterConfig struct {
	// Groups are checked in order; the first one matching a request limits it. A request matching none is not limited.
	Groups []RateLimitGroup
	// ExemptPaths are never limited when the request path equals one of them.
	ExemptPaths []string
	// ExemptPrefixes are never limited when the request path starts with one of them.
	ExemptPrefixes []string
	// ClientIP controls how the client IP of the default key is resolved.
	ClientIP ClientIPConfig

	Redis           RateAllower
	RedisTimeout    time.Duration
	RedisRetryAfter time.Duration
	MeterProvider   metric.MeterProvider

	// now is the clock deciding when Redis is retried; nil means time.Now. Tests replace it.
	now func() time.Time
}

// RateLimiterMiddleware limits requests per client with the first group matching the request. A client over the limit
// gets a 429 response.
func RateLimiterMiddleware(cfg RateLimiterConfig) (fiber.Handler, error) {
	var algorithm limiter.LimiterHandler
	if cfg.Redis != nil {
		now := cfg.now
		if now == nil {
			now = time.Now
		}
		algorithm = &redisRateLimiter{
			redis:   cfg.Redis,
			timeout: cfg.RedisTimeout,
			now:     now,
			backend: newRateLimitBackend(cfg.RedisRetryAfter),
		}
	}

	limiters := make([]fiber.Handler, len(cfg.Groups))
	for i, group := range cfg.Groups {
		if group.Name == "" {
			return nil, fmt.Errorf("rate limit group %d has no name", i)
		}
		if group.Rule.Max <= 0 || group.Rule.Window <= 0 {
			return nil, fmt.Errorf("rate limit group %q needs a positive max and window", group.Name)
		}
		limiters[i] = newGroupLimiter(group, cfg.ClientIP, algorithm)
	}

	return func(c *fiber.Ctx) error {
		if isExempt(c.Path(), cfg.ExemptPaths, cfg.ExemptPrefixes) {
			return c.Next()
		}

		for i, group := range cfg.Groups {
			if group.Match == nil || group.Match(c) {
				return limiters[i](c)
			}
		}

		return c.Next()
	}, nil
}

// newGroupLimiter builds the limiter of one group. A nil algorithm means fiber's in-memory fixed window.
func newGroupLimiter(group RateLimitGroup, clientIP ClientIPConfig, algorithm limiter.LimiterHandler) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:               group.Rule.Max,
		Expiration:        group.Rule.Window,
		LimiterMiddleware: algorithm,
		KeyGenerator: func(c *fiber.Ctx) string {
			var key string
			if group.Key != nil {
				key = group.Key(c)
			}
			if key == "" {
				key = "ip:" + ClientIP(c, clientIP)
			}
			return group.Name + ":" + key
		},
		LimitReached: func(c *fiber.Ctx) error {
			// Fiber's fixed window sets Retry-After before calling this, but sets the X-RateLimit-* headers only on allowed
			// requests. The Redis limiter has already set all of them.
			if c.GetRespHeader("X-RateLimit-Limit") == "" {
				c.Set("X-RateLimit-Limit", strconv.Itoa(group.Rule.Max))
				c.Set("X-RateLimit-Remaining", "0")
				c.Set("X-RateLimit-Reset", c.GetRespHeader(fiber.HeaderRetryAfter))
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"message": LimitedMessage})
		},
	})
}

func isExempt(path string, paths, prefixes []string) bool {
	for _, p := range paths {
		if path == p {
			return true
		}
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
