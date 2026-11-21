// Package redisadapter implements cache ports on top of the Redis cache client.
package redisadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisGo "github.com/redis/go-redis/v9"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/cache"
)

const (
	keyPrefix = "redirect:"
	// absentValue marks a code the datastore does not have.
	absentValue = "-"
)

// redirectEntry is the stored form of a RedirectTarget.
type redirectEntry struct {
	OriginalURL string        `json:"original_url"`
	Status      url.URLStatus `json:"status"`
	ExpiresOn   time.Time     `json:"expires_on"`
}

type redirectCache struct {
	client cache.CacheClient
}

var _ ports.RedirectCache = (*redirectCache)(nil)

// NewRedirectCache stores redirect targets in the cache client.
func NewRedirectCache(client cache.CacheClient) ports.RedirectCache {
	return &redirectCache{client: client}
}

func (rc *redirectCache) Get(ctx context.Context, code string) (url.RedirectTarget, ports.RedirectLookup, error) {
	item, err := rc.client.Get(ctx, keyPrefix+code)
	if errors.Is(err, redisGo.Nil) {
		return url.RedirectTarget{}, ports.RedirectUnknown, nil
	}
	if err != nil {
		return url.RedirectTarget{}, ports.RedirectUnknown, err
	}

	raw, ok := item.Value.(string)
	if !ok {
		return url.RedirectTarget{}, ports.RedirectUnknown, fmt.Errorf("redirect cache: unexpected value type %T", item.Value)
	}
	if raw == absentValue {
		return url.RedirectTarget{}, ports.RedirectAbsent, nil
	}

	var entry redirectEntry
	if unmarshalErr := json.Unmarshal([]byte(raw), &entry); unmarshalErr != nil {
		return url.RedirectTarget{}, ports.RedirectUnknown, fmt.Errorf("redirect cache: decode entry: %w", unmarshalErr)
	}
	target, targetErr := url.NewRedirectTarget(entry.OriginalURL, entry.Status, entry.ExpiresOn)
	if targetErr != nil {
		return url.RedirectTarget{}, ports.RedirectUnknown, fmt.Errorf("redirect cache: invalid entry: %w", targetErr)
	}
	return target, ports.RedirectFound, nil
}

func (rc *redirectCache) Put(ctx context.Context, code string, target url.RedirectTarget, ttl time.Duration) error {
	raw, err := json.Marshal(redirectEntry{
		OriginalURL: target.OriginalURL().Value(),
		Status:      target.Status(),
		ExpiresOn:   target.ExpiresOn(),
	})
	if err != nil {
		return fmt.Errorf("redirect cache: encode entry: %w", err)
	}
	return rc.client.Set(ctx, cache.CacheItem{Key: keyPrefix + code, Value: string(raw), TTL: ttl})
}

func (rc *redirectCache) PutAbsent(ctx context.Context, code string, ttl time.Duration) error {
	return rc.client.Set(ctx, cache.CacheItem{Key: keyPrefix + code, Value: absentValue, TTL: ttl})
}

func (rc *redirectCache) Evict(ctx context.Context, codes ...string) error {
	var errs []error
	for _, code := range codes {
		if code == "" {
			continue
		}
		if err := rc.client.Delete(ctx, keyPrefix+code); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
