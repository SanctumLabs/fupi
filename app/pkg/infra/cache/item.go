package cache

import "time"

// CacheItem represents the item in the cache.
// this can be extended to provide specific implementation for different types of caches
type CacheItem struct {
	// Key is the key in the cache for this item
	Key string

	// Value is the payload or value of the cache item
	Value any

	// TTL is the time to live for this cache item in the cache or the expiry. After which it is removed from the cache
	TTL time.Duration

	// SetX sets an option that only sets the key if it does not already exist.
	SetX bool

	// SetNX sets an option that only sets the key if it does not already exist.
	SetNX bool

	// SkipLocalCache sets an option to skip local cache
	SkipLocalCache bool
}
