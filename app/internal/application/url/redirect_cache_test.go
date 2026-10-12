package urlapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	mockports "github.com/sanctumlabs/fupi/app/internal/ports/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	testTTL       = 5 * time.Minute
	testAbsentTTL = 30 * time.Second
)

// fakeRedirectCache is an in-memory ports.RedirectCache that records the TTLs it was given.
type fakeRedirectCache struct {
	found   map[string]url.RedirectTarget
	absent  map[string]bool
	ttls    map[string]time.Duration
	failing error
}

func newFakeRedirectCache() *fakeRedirectCache {
	return &fakeRedirectCache{found: map[string]url.RedirectTarget{}, absent: map[string]bool{}, ttls: map[string]time.Duration{}}
}

func (f *fakeRedirectCache) Get(_ context.Context, code string) (url.RedirectTarget, ports.RedirectLookup, error) {
	if f.failing != nil {
		return url.RedirectTarget{}, ports.RedirectUnknown, f.failing
	}
	if target, ok := f.found[code]; ok {
		return target, ports.RedirectFound, nil
	}
	if f.absent[code] {
		return url.RedirectTarget{}, ports.RedirectAbsent, nil
	}
	return url.RedirectTarget{}, ports.RedirectUnknown, nil
}

func (f *fakeRedirectCache) Put(_ context.Context, code string, target url.RedirectTarget, ttl time.Duration) error {
	if f.failing != nil {
		return f.failing
	}
	f.found[code], f.ttls[code] = target, ttl
	return nil
}

func (f *fakeRedirectCache) PutAbsent(_ context.Context, code string, ttl time.Duration) error {
	if f.failing != nil {
		return f.failing
	}
	f.absent[code], f.ttls[code] = true, ttl
	return nil
}

func (f *fakeRedirectCache) Evict(_ context.Context, codes ...string) error {
	if f.failing != nil {
		return f.failing
	}
	for _, code := range codes {
		delete(f.found, code)
		delete(f.absent, code)
	}
	return nil
}

func newCachedService(t *testing.T) (*Service, *urlmock.MockUrlDatastore, *fakeRedirectCache) {
	t.Helper()
	ctrl := gomock.NewController(t)
	urls := urlmock.NewMockUrlDatastore(ctrl)
	cache := newFakeRedirectCache()
	return NewService(urls, mockports.NewMockShortCodeGenerator(ctrl), WithRedirectCache(cache, testTTL, testAbsentTTL)), urls, cache
}

func target(t *testing.T, status url.URLStatus, expiresOn time.Time) url.RedirectTarget {
	t.Helper()
	got, err := url.NewRedirectTarget("https://example.com/target/page", status, expiresOn)
	require.NoError(t, err)
	return got
}

func TestResolve_ServesTheSecondRedirectFromTheCacheWithoutTheDatastore(t *testing.T) {
	svc, urls, _ := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "000abc").
		Return(target(t, url.URLStatusActive, time.Now().Add(time.Hour)), nil).Times(1)

	for range 2 {
		got, err := svc.Resolve(context.Background(), "000abc")
		require.NoError(t, err)
		require.Equal(t, "https://example.com/target/page", got.Value())
	}
}

func TestResolve_TheCachedTargetIsReCheckedOnEveryHit(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "000abc").
		Return(target(t, url.URLStatusActive, time.Now().Add(time.Hour)), nil).Times(1)
	_, err := svc.Resolve(context.Background(), "000abc")
	require.NoError(t, err)

	cache.found["000abc"] = target(t, url.URLStatusActive, time.Now().Add(-time.Second))
	_, err = svc.Resolve(context.Background(), "000abc")
	require.True(t, errdefs.IsNotFound(err), "an expired cached target must not redirect, got %v", err)

	cache.found["000abc"] = target(t, url.URLStatusSuspended, time.Now().Add(time.Hour))
	_, err = svc.Resolve(context.Background(), "000abc")
	require.True(t, errdefs.IsNotFound(err), "a suspended cached target must not redirect, got %v", err)
}

func TestResolve_TheCacheTTLNeverOutlivesTheUrl(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "soon").
		Return(target(t, url.URLStatusActive, time.Now().Add(time.Minute)), nil)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "later").
		Return(target(t, url.URLStatusActive, time.Now().Add(24*time.Hour)), nil)

	_, err := svc.Resolve(context.Background(), "soon")
	require.NoError(t, err)
	_, err = svc.Resolve(context.Background(), "later")
	require.NoError(t, err)

	require.InDelta(t, time.Minute.Seconds(), cache.ttls["soon"].Seconds(), 2)
	require.Equal(t, testTTL, cache.ttls["later"])
}

func TestResolve_AnAlreadyExpiredTargetIsNotCached(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "000abc").
		Return(target(t, url.URLStatusActive, time.Now().Add(-time.Hour)), nil)

	_, err := svc.Resolve(context.Background(), "000abc")

	require.True(t, errdefs.IsNotFound(err))
	require.Empty(t, cache.found)
}

func TestResolve_UnknownCodesAreRememberedBriefly(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "nope").
		Return(url.RedirectTarget{}, errdefs.NotFound(errors.New("no rows"))).Times(1)

	for range 2 {
		_, err := svc.Resolve(context.Background(), "nope")
		require.True(t, errdefs.IsNotFound(err))
	}
	require.Equal(t, testAbsentTTL, cache.ttls["nope"])
}

func TestResolve_DatastoreFailuresAreNotCached(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "000abc").Return(url.RedirectTarget{}, errors.New("connection reset"))

	_, err := svc.Resolve(context.Background(), "000abc")

	require.Error(t, err)
	require.Empty(t, cache.found)
	require.Empty(t, cache.absent)
}

func TestResolve_RedirectsWhenTheCacheIsDown(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	cache.failing = errors.New("redis: connection refused")
	urls.EXPECT().FetchRedirectTarget(gomock.Any(), "000abc").
		Return(target(t, url.URLStatusActive, time.Now().Add(time.Hour)), nil)

	got, err := svc.Resolve(context.Background(), "000abc")

	require.NoError(t, err)
	require.Equal(t, "https://example.com/target/page", got.Value())
}

func cachedUrl(t *testing.T, userID string, options ...urlmock.MockUrlOption) url.URL {
	t.Helper()
	defaults := []urlmock.MockUrlOption{urlmock.WithUserId(userID), urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour))}
	u, err := urlmock.MockUrl(append(defaults, options...)...)
	require.NoError(t, err)
	return *u
}

func TestDelete_EvictsTheCodeAndTheAlias(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	owner := "018f4b0e-0000-7000-8000-000000000001"
	owned := cachedUrl(t, owner, urlmock.WithShortCode("000abc"), urlmock.WithCustomAlias("my-alias"))
	cache.found["000abc"] = target(t, url.URLStatusActive, time.Now().Add(time.Hour))
	cache.found["my-alias"] = cache.found["000abc"]
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(owned, nil)
	urls.EXPECT().SoftDelete(gomock.Any(), gomock.Any()).Return(nil)

	require.NoError(t, svc.Delete(context.Background(), owner, owned.ID().String()))

	require.Empty(t, cache.found)
}

func TestUpdate_EvictsTheCodeAndTheAlias(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	owner := "018f4b0e-0000-7000-8000-000000000001"
	owned := cachedUrl(t, owner, urlmock.WithShortCode("000abc"), urlmock.WithCustomAlias("my-alias"))
	cache.found["000abc"] = target(t, url.URLStatusActive, time.Now().Add(time.Hour))
	cache.found["my-alias"] = cache.found["000abc"]
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(owned, nil)
	urls.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) { return u, nil })
	expiry := time.Now().Add(72 * time.Hour)

	_, err := svc.Update(context.Background(), UpdateCommand{UserID: owner, ID: owned.ID().String(), ExpiresOn: &expiry})

	require.NoError(t, err)
	require.Empty(t, cache.found)
}

func TestShorten_ForgetsAnAbsentMarkerForTheNewCode(t *testing.T) {
	ctrl := gomock.NewController(t)
	urls := urlmock.NewMockUrlDatastore(ctrl)
	generator := mockports.NewMockShortCodeGenerator(ctrl)
	cache := newFakeRedirectCache()
	cache.absent["000abc"] = true
	svc := NewService(urls, generator, WithRedirectCache(cache, testTTL, testAbsentTTL))
	code, err := url.NewShortCode("000abc")
	require.NoError(t, err)
	generator.EXPECT().Next(gomock.Any()).Return(code, nil)
	urls.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) { return u, nil })

	_, err = svc.Shorten(context.Background(), ShortenCommand{
		UserID:      "018f4b0e-0000-7000-8000-000000000001",
		OriginalURL: "https://example.com/target/page",
		ExpiresOn:   time.Now().Add(time.Hour),
	})

	require.NoError(t, err)
	require.False(t, cache.absent["000abc"])
}
