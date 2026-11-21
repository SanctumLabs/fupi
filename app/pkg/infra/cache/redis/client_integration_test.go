//go:build integration

package redis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/cache"
	cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	redisContainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func startRedis(t *testing.T) (cache.CacheClient, *redisContainer.RedisContainer) {
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

	return client, container
}

func TestRedisClient_SetGetExistsDelete(t *testing.T) {
	ctx := context.Background()
	client, _ := startRedis(t)

	require.NoError(t, client.Ping(ctx))
	require.NoError(t, client.Set(ctx, cache.CacheItem{Key: "short:abc", Value: "https://example.com"}))

	assert.True(t, client.Exists(ctx, "short:abc"))
	item, err := client.Get(ctx, "short:abc")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", item.Value)

	require.NoError(t, client.Delete(ctx, "short:abc"))
	assert.False(t, client.Exists(ctx, "short:abc"))
	_, err = client.Get(ctx, "short:abc")
	assert.Error(t, err, "a missing key is an error, not an empty item")
}

func TestRedisClient_ItemsExpireAfterTheirTTL(t *testing.T) {
	ctx := context.Background()
	client, _ := startRedis(t)

	require.NoError(t, client.Set(ctx, cache.CacheItem{Key: "k", Value: "v"}, cache.WithTTL(time.Second)))
	require.True(t, client.Exists(ctx, "k"))

	require.Eventually(t, func() bool { return !client.Exists(ctx, "k") }, 5*time.Second, 100*time.Millisecond)
}

func TestRedisClient_PingReportsAStoppedRedis(t *testing.T) {
	ctx := context.Background()
	client, container := startRedis(t)
	require.NoError(t, client.Ping(ctx))

	require.NoError(t, container.Stop(ctx, nil))

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	assert.Error(t, client.Ping(pingCtx))
}

// A Redis key can be a user's URL and a value a token, so a span records which command ran and nothing else.
func TestRedisClient_TracesCommandsWithoutTheirKeysOrValues(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	client, _ := startRedis(t) // created after the provider is global, as in main
	ctx := context.Background()

	const key, value = "user:secret-key-123", "secret-value-456"
	requestCtx, request := provider.Tracer("test").Start(ctx, "request")
	require.NoError(t, client.Set(requestCtx, cache.CacheItem{Key: key, Value: value}))
	got, err := client.Get(requestCtx, key)
	require.NoError(t, err)
	request.End()
	require.Equal(t, value, got.Value)

	var commands []string
	for _, span := range exporter.GetSpans() {
		text := span.Name
		for _, attribute := range span.Attributes {
			text += " " + string(attribute.Key) + "=" + attribute.Value.String()
		}
		for _, event := range span.Events {
			text += " " + event.Name
			for _, attribute := range event.Attributes {
				text += " " + attribute.Value.String()
			}
		}
		assert.NotContains(t, text, "secret-key-123", "span %q", span.Name)
		assert.NotContains(t, text, "secret-value-456", "span %q", span.Name)
		if span.SpanContext.TraceID() == request.SpanContext().TraceID() && span.Name != "request" {
			commands = append(commands, strings.ToLower(span.Name))
		}
	}
	assert.Contains(t, strings.Join(commands, " "), "set", "the SET is a child span of the request")
	assert.Contains(t, strings.Join(commands, " "), "get", "the GET is a child span of the request")
}
