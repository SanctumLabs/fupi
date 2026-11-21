package redisadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	redisGo "github.com/redis/go-redis/v9"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/cache"
	mockcache "github.com/sanctumlabs/fupi/app/pkg/infra/cache/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestRedirectCache_RoundTripsATarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mockcache.NewMockCacheClient(ctrl)
	rc := NewRedirectCache(client)
	expiresOn := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	target, err := url.NewRedirectTarget("https://example.com/target/page", url.URLStatusActive, expiresOn)
	require.NoError(t, err)

	var stored cache.CacheItem
	client.EXPECT().Set(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item cache.CacheItem, _ ...cache.CacheItemOption) error {
		stored = item
		return nil
	})
	require.NoError(t, rc.Put(context.Background(), "000abc", target, time.Minute))
	require.Equal(t, "redirect:000abc", stored.Key)
	require.Equal(t, time.Minute, stored.TTL)

	client.EXPECT().Get(gomock.Any(), "redirect:000abc").Return(cache.CacheItem{Value: stored.Value}, nil)
	got, lookup, err := rc.Get(context.Background(), "000abc")

	require.NoError(t, err)
	require.Equal(t, ports.RedirectFound, lookup)
	require.Equal(t, "https://example.com/target/page", got.OriginalURL().Value())
	require.Equal(t, url.URLStatusActive, got.Status())
	require.True(t, expiresOn.Equal(got.ExpiresOn()))
}

func TestRedirectCache_MissAbsentAndFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mockcache.NewMockCacheClient(ctrl)
	rc := NewRedirectCache(client)

	client.EXPECT().Get(gomock.Any(), "redirect:miss").Return(cache.CacheItem{}, redisGo.Nil)
	_, lookup, err := rc.Get(context.Background(), "miss")
	require.NoError(t, err)
	require.Equal(t, ports.RedirectUnknown, lookup)

	client.EXPECT().Get(gomock.Any(), "redirect:nope").Return(cache.CacheItem{Value: absentValue}, nil)
	_, lookup, err = rc.Get(context.Background(), "nope")
	require.NoError(t, err)
	require.Equal(t, ports.RedirectAbsent, lookup)

	client.EXPECT().Get(gomock.Any(), "redirect:down").Return(cache.CacheItem{}, errors.New("connection refused"))
	_, lookup, err = rc.Get(context.Background(), "down")
	require.Error(t, err)
	require.Equal(t, ports.RedirectUnknown, lookup)

	client.EXPECT().Get(gomock.Any(), "redirect:junk").Return(cache.CacheItem{Value: "{not json"}, nil)
	_, lookup, err = rc.Get(context.Background(), "junk")
	require.Error(t, err)
	require.Equal(t, ports.RedirectUnknown, lookup)
}

func TestRedirectCache_EvictSkipsEmptyCodesAndKeepsGoing(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mockcache.NewMockCacheClient(ctrl)
	rc := NewRedirectCache(client)
	client.EXPECT().Delete(gomock.Any(), "redirect:000abc").Return(errors.New("boom"))
	client.EXPECT().Delete(gomock.Any(), "redirect:alias").Return(nil)

	err := rc.Evict(context.Background(), "000abc", "", "alias")

	require.Error(t, err)
}
