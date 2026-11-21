package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/tools/logger"
	"github.com/sanctumlabs/curtz/app/tools/monitoring"
)

var log = logger.NewLogger("cache")

// Cache represents a cache
type Cache struct {
	client redis.UniversalClient
	ctx    context.Context
}

// New creates a new cache service
func New(config config.CacheConfig) *Cache {
	defer monitoring.ErrorHandler()

	ctx := context.Background()
	var options redis.UniversalOptions

	if config.RequireAuth {
		options = redis.UniversalOptions{
			Password: config.Password,
			Username: config.Username,
			Addrs:    []string{fmt.Sprintf("%s:%s", config.Host, config.Port)},
		}
	} else {
		options = redis.UniversalOptions{
			Addrs: []string{fmt.Sprintf(":%s", config.Port)},
		}
	}

	redisClient := redis.NewUniversalClient(&options)

	cmd := redisClient.Ping(ctx)
	if cmd.Err() != nil {
		log.Errorf("Failed to connect to cache: %v", cmd.Err())
	}

	log.Infof("Connected to cache at host %s", config.Host)

	return &Cache{
		client: redisClient,
		ctx:    ctx,
	}
}

// LookupUrl looks up a url given its short code from the cache
func (c *Cache) LookupUrl(shortCode string) (string, error) {
	defer monitoring.ErrorHandler()

	originalUrl, err := c.client.Get(c.ctx, shortCode).Result()

	if err != nil {
		return "", err
	}

	return originalUrl, nil
}

// SaveURL saves a new url in the cache with the short code as the key and original url value
func (c *Cache) SaveURL(shortCode, originalUrl string, expiryTime time.Duration) (string, error) {
	defer monitoring.ErrorHandler()

	cmd, err := c.client.Set(c.ctx, shortCode, originalUrl, expiryTime).Result()

	if err != nil {
		return "", err
	}

	return cmd, nil
}
