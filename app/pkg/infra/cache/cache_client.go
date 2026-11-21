package cache

import (
	"context"
)

// CacheClient defines a method set for a caching client. This allows the usage of different caching implementations
// without 'leaking' the underlying implementation to the caller/business logic
type CacheClient interface {

	// Set adds a cache item to the cache with additional options
	Set(context.Context, CacheItem, ...CacheItemOption) error

	// Get retrieves a value from the cache with a given key
	// Pass in a payloadType pointer to the payloadType argument
	Get(ctx context.Context, key string) (CacheItem, error)

	// Exists checks if a value for a given key exists in the cache
	Exists(ctx context.Context, key string) bool

	// Delete removes a given value with a given key from the cache
	Delete(ctx context.Context, key string) error
}
