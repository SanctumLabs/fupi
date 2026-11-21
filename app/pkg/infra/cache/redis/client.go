package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/wire"
	redisGo "github.com/redis/go-redis/v9"
	"github.com/sanctumlabs/curtz/app/pkg/infra/cache"
)

const (
	_statsEnabled        = true
	_defaultConnAttempts = 3
	_defaultConnTimeout  = time.Second
)

// redisClient is a wrapper around
type redisClient struct {
	connAttempts int
	connTimeout  time.Duration

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

// NewRedisClient creates a new redis client
func NewRedisClient(config RedisClientConfig) (cache.CacheClient, error) {
	rc := &redisClient{
		connAttempts: _defaultConnAttempts,
		connTimeout:  _defaultConnTimeout,
	}

	var err error
	for rc.connAttempts > 0 {
		options := &redisGo.UniversalOptions{
			Addrs:      config.Address,
			Username:   config.Username,
			Password:   config.Password,
			DB:         config.Database,
			MasterName: config.MasterName,
		}

		client := redisGo.NewUniversalClient(options)
		rc.client = client

		statusCmd := client.Ping(context.Background())
		err = statusCmd.Err()
		if err != nil {
			slog.Error(fmt.Sprintf("RedisClient> 🚫 Redis failed to connect with error %s, attempts left: %d", statusCmd, rc.connAttempts))
			break
		}

		slog.Warn(fmt.Sprintf("RedisClient> Redis is trying to connect, attempts left: %d", rc.connAttempts))

		time.Sleep(rc.connTimeout)

		rc.connAttempts--
	}
	if err != nil {
		slog.Error(fmt.Sprintf("RedisClient> 🚫 failed to connect to Redis, Error: %s", err))
		return nil, err
	}

	slog.Info(
		"RedisClient> ✅ connected to Redis",
		"host", config.Host,
		"port", config.Port,
	)
	return rc, nil
}

func (p *redisClient) Configure(opts ...Option) cache.CacheClient {
	for _, opt := range opts {
		opt(p)
	}

	return p
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
