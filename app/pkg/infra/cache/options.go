package cache

import (
	"time"
)

// CacheItemOption sets optional for adding to the cached item
type CacheItemOption func(item *CacheItem)

// WithTTL sets the duration for how long to cache an item
func WithTTL(ttl time.Duration) CacheItemOption {
	return func(item *CacheItem) {
		item.TTL = ttl
	}
}

// WithSetX sets an option that only sets the key if it already exists.
func WithSetX(setX bool) CacheItemOption {
	return func(item *CacheItem) {
		item.SetX = setX
	}
}

// WithSetNX sets an option that only sets the key if it does not already exist.
func WithSetNX(setNx bool) CacheItemOption {
	return func(item *CacheItem) {
		item.SetNX = setNx
	}
}

// WithSkipLocalCache sets an option to skip local cache as if it is not set.
func WithSkipLocalCache(skipLocalCache bool) CacheItemOption {
	return func(item *CacheItem) {
		item.SkipLocalCache = skipLocalCache
	}
}
