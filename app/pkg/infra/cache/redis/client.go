package redis

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/wire"
	"github.com/redis/go-redis/extra/redisotel/v9"
	redisGo "github.com/redis/go-redis/v9"
	"github.com/sanctumlabs/fupi/app/pkg/infra/cache"
)

const _statsEnabled = true

// redisClient is a wrapper around a go-redis universal client
type redisClient struct {
	// statsEnabled sets enabling stats to true
	statsEnabled bool

	// marshalFunc a marshaling function that marshals/serializes a value into a byte slice
	marshalFunc func(any) ([]byte, error)

	// unmarshalFunc un-marshals a byte slice into a given payload type
	unmarshalFunc func([]byte, any) error

	// client
	client redisGo.UniversalClient
}

var (
	_             cache.CacheClient = (*redisClient)(nil)
	RedisCacheSet                   = wire.NewSet(NewRedisClient)
)

// NewRedisClient builds a client for the configured addresses: one address gives a plain client, several give a
// cluster client. It performs no I/O. go-redis dials lazily and reconnects by itself, so Redis may come up after the
// caller and the client picks it up; use Ping to check reachability.
func NewRedisClient(config RedisClientConfig) (cache.CacheClient, error) {
	if len(config.Address) == 0 {
		return nil, errors.New("redis: at least one address is required")
	}

	client := redisGo.NewUniversalClient(&redisGo.UniversalOptions{
		Addrs:      config.Address,
		Username:   config.Username,
		Password:   config.Password,
		DB:         config.Database,
		MasterName: config.MasterName,
		// Without it go-redis ignores a context deadline and waits for its own read timeout, so a caller cannot bound
		// a command on a hung Redis.
		ContextTimeoutEnabled: true,
	})

	// A span and a metric per command. The command text is switched off: keys and values can hold URLs and tokens (D7).
	// Instrumenting only registers hooks; a failure costs the telemetry, not the client.
	if err := redisotel.InstrumentTracing(client, redisotel.WithDBStatement(false)); err != nil {
		slog.Warn("redis: tracing is not enabled", "error", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		slog.Warn("redis: metrics are not enabled", "error", err)
	}

	return &redisClient{client: client}, nil
}

func (p *redisClient) Configure(opts ...Option) cache.CacheClient {
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Ping checks that Redis answers
func (rc *redisClient) Ping(ctx context.Context) error {
	return rc.client.Ping(ctx).Err()
}

// Close closes the connections held by the client
func (rc *redisClient) Close() error {
	return rc.client.Close()
}

// Set adds an item with a given key to the cache
func (rc *redisClient) Set(ctx context.Context, item cache.CacheItem, options ...cache.CacheItemOption) error {
	// apply optional options for caching item
	for _, option := range options {
		option(&item)
	}

	// cache the item
	statusCmd := rc.client.Set(ctx, item.Key, item.Value, item.TTL)

	if statusCmd.Err() != nil {
		return statusCmd.Err()
	}

	return nil
}

// Get retrieves a value from the cache with a given key
func (rc *redisClient) Get(ctx context.Context, key string) (cache.CacheItem, error) {
	statusCmd := rc.client.Get(ctx, key)
	err := statusCmd.Err()
	if err != nil {
		return cache.CacheItem{}, err
	}

	statusCmd.Val()

	item := cache.CacheItem{
		Key:   key,
		Value: statusCmd.Val(),
	}

	return item, nil
}

// Exists checks if a value for a given key exists in the cache
func (rc *redisClient) Exists(ctx context.Context, key string) bool {
	statusCmd := rc.client.Exists(ctx, key)
	return statusCmd.Val() > 0
}

// Delete deletes a value from the cache with a given key
func (rc *redisClient) Delete(ctx context.Context, key string) error {
	statusCmd := rc.client.Del(ctx, key)
	return statusCmd.Err()
}

// UniversalClient returns the go-redis client behind a cache client built by NewRedisClient, for callers that need
// commands the cache interface does not offer. It reports false for any other cache client.
func UniversalClient(client cache.CacheClient) (redisGo.UniversalClient, bool) {
	rc, ok := client.(*redisClient)
	if !ok {
		return nil, false
	}
	return rc.client, true
}
