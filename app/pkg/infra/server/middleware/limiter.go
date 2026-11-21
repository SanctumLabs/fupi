package middleware

import (
	"context"
	"fmt"
	"net/netip"
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
	// client IP; an IPv6 client is keyed by its /64.
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

	// Redis, when set, decides requests; RedisTimeout bounds each call to it, and after an error RedisRetryAfter is how
	// long the limiter decides in memory before asking Redis again. Both must be positive when Redis is set.
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
	var backend *rateLimitBackend
	if cfg.Redis != nil {
		if cfg.RedisTimeout <= 0 {
			return nil, fmt.Errorf("rate limiter needs a positive redis timeout, got %s", cfg.RedisTimeout)
		}
		if cfg.RedisRetryAfter <= 0 {
			return nil, fmt.Errorf("rate limiter needs a positive redis retry after, got %s", cfg.RedisRetryAfter)
		}
		backend = newRateLimitBackend(cfg.RedisRetryAfter)
	}
	metrics, err := newLimiterMetrics(cfg.MeterProvider, backend)
	if err != nil {
		return nil, fmt.Errorf("rate limiter metrics: %w", err)
	}

	now := cfg.now
	if now == nil {
		now = time.Now
	}

	limiters := make([]fiber.Handler, len(cfg.Groups))
	for i, group := range cfg.Groups {
		if group.Name == "" {
			return nil, fmt.Errorf("rate limit group %d has no name", i)
		}
		if group.Rule.Max <= 0 || group.Rule.Window <= 0 {
			return nil, fmt.Errorf("rate limit group %q needs a positive max and window", group.Name)
		}

		algorithm := limiter.LimiterHandler(&memoryRateLimiter{group: group.Name, metrics: metrics})
		if cfg.Redis != nil {
			algorithm = &redisRateLimiter{
				group:   group.Name,
				redis:   cfg.Redis,
				timeout: cfg.RedisTimeout,
				now:     now,
				backend: backend,
				metrics: metrics,
			}
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

// memoryRateLimiter is a limiter.LimiterHandler for one group without Redis: fiber's fixed window, with its decisions
// recorded.
type memoryRateLimiter struct {
	group   string
	metrics *limiterMetrics
}

// New returns the handler for one group.
func (m *memoryRateLimiter) New(cfg limiter.Config) fiber.Handler {
	window := limiter.FixedWindow{}.New(cfg)
	return func(c *fiber.Ctx) error {
		return m.metrics.inMemory(c, window, m.group, cfg.KeyGenerator(c))
	}
}

// newGroupLimiter builds the limiter of one group, deciding requests with algorithm.
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
				key = "ip:" + ipKey(ClientIP(c, clientIP))
			}
			return group.Name + ":" + key
		},
		LimitReached: func(c *fiber.Ctx) error {
			c.Locals(limitedKey{}, true)
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

// ipKey is the part of a client IP that identifies the client: the whole IPv4 address, or the /64 of an IPv6 one, since
// one host is usually handed a whole /64 and could otherwise take a fresh bucket for every address in it. An address that
// does not parse is kept as is.
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
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
