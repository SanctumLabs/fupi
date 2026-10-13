package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAllower returns a scripted result or error and records what it was asked. When release is set, each call waits
// for it to close, or for its context to end like with a hung Redis, before answering.
type fakeAllower struct {
	calls atomic.Int32

	mu      sync.Mutex
	result  *redis_rate.Result
	err     error
	release chan struct{}
	keys    []string
	limits  []redis_rate.Limit
}

func (f *fakeAllower) Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error) {
	f.calls.Add(1)

	f.mu.Lock()
	f.keys = append(f.keys, key)
	f.limits = append(f.limits, limit)
	result, err, release := f.result, f.err, f.release
	f.mu.Unlock()

	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, err
}

func (f *fakeAllower) answer(result *redis_rate.Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result, f.err = result, err
}

// hold makes calls wait until the returned function is called.
func (f *fakeAllower) hold() (releaseAll func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.release = make(chan struct{})
	return func() { close(f.release) }
}

// testClock is a clock the test advances by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var errRedisDown = errors.New("redis down")

// redisLimitedConfig is a single "auth" group limited by fake, with a clock that only moves when the test says so.
func redisLimitedConfig(fake *fakeAllower, clock *testClock, max int) RateLimiterConfig {
	return RateLimiterConfig{
		Groups:          []RateLimitGroup{{Name: "auth", Rule: RateLimitRule{Max: max, Window: time.Minute}}},
		Redis:           fake,
		RedisTimeout:    time.Second,
		RedisRetryAfter: 5 * time.Second,
		now:             clock.Now,
	}
}

func TestRedisLimiter_AllowedSetsHeaders(t *testing.T) {
	fake := &fakeAllower{result: &redis_rate.Result{Allowed: 1, Remaining: 4, ResetAfter: 1500 * time.Millisecond}}
	app := newLimitedApp(t, redisLimitedConfig(fake, newTestClock(), 5))

	resp := serve(t, app, "/x", testPeer, nil)

	assert.Equal(t, fiber.StatusOK, resp.StatusCode())
	assert.Equal(t, "5", string(resp.Header.Peek("X-RateLimit-Limit")))
	assert.Equal(t, "4", string(resp.Header.Peek("X-RateLimit-Remaining")))
	assert.Equal(t, "2", string(resp.Header.Peek("X-RateLimit-Reset")))
	assert.Empty(t, resp.Header.Peek(fiber.HeaderRetryAfter))

	assert.Equal(t, []string{"auth:ip:" + testPeer}, fake.keys)
	assert.Equal(t, []redis_rate.Limit{{Rate: 5, Burst: 5, Period: time.Minute}}, fake.limits)
}

func TestRedisLimiter_DeniedReturns429WithRetryAfter(t *testing.T) {
	fake := &fakeAllower{result: &redis_rate.Result{
		Allowed:    0,
		Remaining:  0,
		RetryAfter: 2100 * time.Millisecond,
		ResetAfter: 11500 * time.Millisecond,
	}}
	app := newLimitedApp(t, redisLimitedConfig(fake, newTestClock(), 5))

	resp := serve(t, app, "/x", testPeer, nil)

	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode())
	assert.Equal(t, "3", string(resp.Header.Peek(fiber.HeaderRetryAfter)))
	assert.Equal(t, "5", string(resp.Header.Peek("X-RateLimit-Limit")))
	assert.Equal(t, "0", string(resp.Header.Peek("X-RateLimit-Remaining")))
	assert.Equal(t, "12", string(resp.Header.Peek("X-RateLimit-Reset")), "reset comes from ResetAfter, not Retry-After")

	var body map[string]string
	require.NoError(t, json.Unmarshal(resp.Body(), &body))
	assert.Equal(t, "too many requests", body["message"])
}

func TestRedisLimiter_DeniedRetryAfterIsAtLeastOneSecond(t *testing.T) {
	fake := &fakeAllower{result: &redis_rate.Result{Allowed: 0, RetryAfter: time.Millisecond}}
	app := newLimitedApp(t, redisLimitedConfig(fake, newTestClock(), 5))

	resp := serve(t, app, "/x", testPeer, nil)

	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode())
	assert.Equal(t, "1", string(resp.Header.Peek(fiber.HeaderRetryAfter)))
}

func TestRedisLimiter_ErrorFallsBackToMemory(t *testing.T) {
	logTo(t, slog.LevelInfo)
	fake := &fakeAllower{err: errRedisDown}
	app := newLimitedApp(t, redisLimitedConfig(fake, newTestClock(), 2))

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/x", testPeer, nil).StatusCode())

	assert.EqualValues(t, 1, fake.calls.Load(), "requests during the cooldown skip Redis")
}

func TestRedisLimiter_RetriesRedisAfterCooldown(t *testing.T) {
	logTo(t, slog.LevelInfo)
	fake := &fakeAllower{err: errRedisDown}
	clock := newTestClock()
	backend := newRateLimitBackend(5 * time.Second)
	handler := limiter.New(limiter.Config{
		Max:          5,
		Expiration:   time.Minute,
		KeyGenerator: func(*fiber.Ctx) string { return "auth:ip:" + testPeer },
		LimiterMiddleware: &redisRateLimiter{
			redis:   fake,
			timeout: time.Second,
			now:     clock.Now,
			backend: backend,
		},
	})
	app := fiber.New()
	app.Use(handler)
	app.All("/*", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.True(t, backend.active())

	clock.advance(4 * time.Second)
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.EqualValues(t, 1, fake.calls.Load(), "Redis is not retried before the cooldown ends")

	fake.answer(&redis_rate.Result{Allowed: 1, Remaining: 4, ResetAfter: time.Second}, nil)
	clock.advance(time.Second)
	resp := serve(t, app, "/x", testPeer, nil)

	assert.Equal(t, fiber.StatusOK, resp.StatusCode())
	assert.EqualValues(t, 2, fake.calls.Load(), "Redis is retried once the cooldown ends")
	assert.Equal(t, "4", string(resp.Header.Peek("X-RateLimit-Remaining")))
	assert.False(t, backend.active())
}

func TestRedisLimiter_HungRedisDoesNotStall(t *testing.T) {
	logTo(t, slog.LevelInfo)
	fake := &fakeAllower{release: make(chan struct{})}
	cfg := redisLimitedConfig(fake, newTestClock(), 5)
	cfg.RedisTimeout = 20 * time.Millisecond
	app := newLimitedApp(t, cfg)

	start := time.Now()
	resp := serve(t, app, "/x", testPeer, nil)

	assert.Equal(t, fiber.StatusOK, resp.StatusCode())
	assert.Less(t, time.Since(start), 200*time.Millisecond)
	assert.EqualValues(t, 1, fake.calls.Load())
}

func TestRedisLimiter_LogsTransitionsOnce(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	fake := &fakeAllower{err: errRedisDown}
	clock := newTestClock()
	cfg := redisLimitedConfig(fake, clock, 1000)
	cfg.RedisTimeout = 10 * time.Second
	app := newLimitedApp(t, cfg)

	// serveTogether sends n concurrent requests and holds their Redis calls until all n are in flight, so they all
	// reach the backend transition at once.
	serveTogether := func(n int) {
		calls := fake.calls.Load()
		releaseAll := fake.hold()
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				serve(t, app, "/x", testPeer, nil)
			}()
		}
		require.Eventually(t, func() bool { return fake.calls.Load() == calls+int32(n) }, 5*time.Second, time.Millisecond)
		releaseAll()
		wg.Wait()
	}

	serveTogether(50)

	clock.advance(time.Minute)
	fake.answer(&redis_rate.Result{Allowed: 1, Remaining: 999, ResetAfter: time.Second}, nil)
	serveTogether(50)

	counts := map[string]int{}
	for _, line := range logLines(t, buf) {
		counts[line["level"].(string)+" "+line["msg"].(string)]++
	}
	assert.Equal(t, map[string]int{
		"WARN rate limiter falling back to memory": 1,
		"INFO rate limiter back on redis":          1,
	}, counts)
}
