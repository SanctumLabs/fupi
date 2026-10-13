//go:build integration

package middleware

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	redisContainer "github.com/testcontainers/testcontainers-go/modules/redis"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
)

// startRedisLimiter starts Redis and returns the rate limiter on it, and the container to break it with.
func startRedisLimiter(t *testing.T) (*redis_rate.Limiter, *redisContainer.RedisContainer) {
	t.Helper()
	ctx := context.Background()

	container, err := redisContainer.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	uri, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	client, err := cacheredis.NewRedisClient(cacheredis.RedisClientConfig{Address: []string{strings.TrimPrefix(uri, "redis://")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	universal, ok := cacheredis.UniversalClient(client)
	require.True(t, ok)

	return redis_rate.NewLimiter(universal), container
}

func TestRateLimiterRedis_SharedAcrossInstances(t *testing.T) {
	limiter, _ := startRedisLimiter(t)
	cfg := RateLimiterConfig{
		Groups:          []RateLimitGroup{{Name: "auth", Rule: RateLimitRule{Max: 10, Window: time.Hour}}},
		Redis:           limiter,
		RedisTimeout:    time.Second,
		RedisRetryAfter: time.Second,
	}
	apps := []*fiber.App{newLimitedApp(t, cfg), newLimitedApp(t, cfg)}

	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if serve(t, apps[i%2], "/x", testPeer, nil).StatusCode() == fiber.StatusOK {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 10, ok.Load(), "two instances share the bucket of one client")
}

// A hung Redis must not stall requests: they fall back to memory after the timeout, and Redis takes over again once it
// answers.
func TestRateLimiterRedis_HungRedisFallsBackAndRecovers(t *testing.T) {
	ctx := context.Background()
	limiter, container := startRedisLimiter(t)

	reader := sdkmetric.NewManualReader()
	app := newLimitedApp(t, RateLimiterConfig{
		Groups:          []RateLimitGroup{{Name: "auth", Rule: RateLimitRule{Max: 100, Window: time.Hour}}},
		Redis:           limiter,
		RedisTimeout:    50 * time.Millisecond,
		RedisRetryAfter: time.Second,
		MeterProvider:   sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	})
	telemetry := &limiterTelemetry{reader: reader}

	require.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	require.EqualValues(t, 0, gaugeValue(t, telemetry.collect(t)["http.ratelimit.fallback.active"]))

	// DEBUG SLEEP is rejected by the stock image, and CLIENT PAUSE makes the other clients' commands block just the same.
	code, _, err := container.Exec(ctx, []string{"redis-cli", "CLIENT", "PAUSE", "2000", "ALL"})
	require.NoError(t, err)
	require.Zero(t, code)
	paused := time.Now()

	start := time.Now()
	resp := serve(t, app, "/x", testPeer, nil)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode())
	assert.Less(t, time.Since(start), 500*time.Millisecond, "a hung Redis costs a request the timeout, not the pause")
	metrics := telemetry.collect(t)
	assert.EqualValues(t, 1, gaugeValue(t, metrics["http.ratelimit.fallback.active"]))
	assert.Equal(t, map[string]int64{"group=auth": 1}, counts(t, metrics["http.ratelimit.backend.errors"]))

	// The pause ends 2s after it began, and Redis is tried again one retry interval after the last failure.
	time.Sleep(time.Until(paused.Add(2*time.Second + 1500*time.Millisecond)))
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())

	metrics = telemetry.collect(t)
	assert.EqualValues(t, 0, gaugeValue(t, metrics["http.ratelimit.fallback.active"]))
	assert.Equal(t, map[string]int64{
		"backend=redis,group=auth,outcome=allowed":  2,
		"backend=memory,group=auth,outcome=allowed": 1,
	}, counts(t, metrics["http.ratelimit.requests"]))
}
