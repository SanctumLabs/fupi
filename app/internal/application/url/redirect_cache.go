package urlapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// redirectCache is the Service's use of a ports.RedirectCache. The zero value is a disabled cache.
type redirectCache struct {
	cache     ports.RedirectCache
	ttl       time.Duration
	absentTTL time.Duration
}

// redirectTarget returns the target of a short code or custom alias, from the cache when it can and from the datastore
// when it cannot. A cache failure is logged and otherwise ignored: Redis is optional (ADR-0015).
func (svc *Service) redirectTarget(ctx context.Context, code string) (url.RedirectTarget, error) {
	cache := svc.redirectCache
	if cache.cache == nil {
		return svc.urls.FetchRedirectTarget(ctx, code)
	}

	target, lookup, getErr := cache.cache.Get(ctx, code)
	if getErr != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s<redirectTarget> Redirect cache read failed", svc.logPrefix), "code", code, "error", getErr)
	}
	switch lookup {
	case ports.RedirectFound:
		return target, nil
	case ports.RedirectAbsent:
		return url.RedirectTarget{}, errdefs.NotFound(fmt.Errorf("url %q not found", code))
	}

	target, fetchErr := svc.urls.FetchRedirectTarget(ctx, code)
	switch {
	case errdefs.IsNotFound(fetchErr):
		svc.cacheWrite(ctx, code, func() error { return cache.cache.PutAbsent(ctx, code, cache.absentTTL) })
	case fetchErr == nil:
		// Never past the expiry, so a target is not served from the cache after the datastore would refuse it.
		ttl := min(cache.ttl, time.Until(target.ExpiresOn()))
		if ttl > 0 {
			svc.cacheWrite(ctx, code, func() error { return cache.cache.Put(ctx, code, target, ttl) })
		}
	}
	return target, fetchErr
}

// evictRedirects forgets the codes after the url they belong to changed or was created. It runs after the datastore
// committed; a failure is logged, and the entries then live until their TTL.
func (svc *Service) evictRedirects(ctx context.Context, u *url.URL) {
	if svc.redirectCache.cache == nil {
		return
	}
	alias := u.CustomAlias()
	svc.cacheWrite(ctx, u.ShortCode().Value(), func() error {
		return svc.redirectCache.cache.Evict(ctx, u.ShortCode().Value(), alias.Value())
	})
}

func (svc *Service) cacheWrite(ctx context.Context, code string, write func() error) {
	if err := write(); err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s Redirect cache write failed", svc.logPrefix), "code", code, "error", err)
	}
}
