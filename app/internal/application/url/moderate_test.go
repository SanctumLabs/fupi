package urlapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const moderatorOwner = "018f4b0e-0000-7000-8000-000000000001"

// expectUpdate makes the datastore accept an Update and hands back the url it was given, so a test can see its events.
func expectUpdate(urls *urlmock.MockUrlDatastore, saved *url.URL) {
	urls.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			*saved = u
			return u, nil
		})
}

func TestSuspend_SuspendsAnActiveUrlAndEvictsItsCodeAndAlias(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	active := cachedUrl(t, moderatorOwner, urlmock.WithShortCode("000abc"), urlmock.WithCustomAlias("my-alias"))
	cache.found["000abc"] = target(t, url.URLStatusActive, time.Now().Add(time.Hour))
	cache.found["my-alias"] = cache.found["000abc"]
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(active, nil)
	var saved url.URL
	expectUpdate(urls, &saved)

	err := svc.Suspend(context.Background(), active.ID().String(), url.SuspensionReasonPhishing)

	require.NoError(t, err)
	require.Equal(t, url.URLStatusSuspended, saved.Status())
	require.Equal(t, []string{"url.suspended"}, eventTypes(saved))
	require.Empty(t, cache.found)
}

func TestSuspend_AUrlThatIsNotActiveIsAConflictAndNothingIsWritten(t *testing.T) {
	svc, urls, _ := newCachedService(t)
	suspended := cachedUrl(t, moderatorOwner, urlmock.WithStatus(url.URLStatusSuspended))
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(suspended, nil)

	err := svc.Suspend(context.Background(), suspended.ID().String(), url.SuspensionReasonMalware)

	require.True(t, errdefs.IsConflict(err), "got %v", err)
}

func TestSuspend_AnUnknownUrlIsNotFound(t *testing.T) {
	svc, urls, _ := newCachedService(t)
	urls.EXPECT().FetchById(gomock.Any(), "018f4b0e-0000-7000-8000-000000000003").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))

	err := svc.Suspend(context.Background(), "018f4b0e-0000-7000-8000-000000000003", url.SuspensionReasonMalware)

	require.True(t, errdefs.IsNotFound(err), "got %v", err)
}

func TestSuspend_ACacheFailureDoesNotFailTheSuspension(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	cache.failing = errors.New("redis: connection refused")
	active := cachedUrl(t, moderatorOwner)
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(active, nil)
	var saved url.URL
	expectUpdate(urls, &saved)

	require.NoError(t, svc.Suspend(context.Background(), active.ID().String(), url.SuspensionReasonSpam))
	require.Equal(t, url.URLStatusSuspended, saved.Status())
}

func TestReinstate_ReinstatesASuspendedUrlAndEvictsItsCodeAndAlias(t *testing.T) {
	svc, urls, cache := newCachedService(t)
	suspended := cachedUrl(t, moderatorOwner, urlmock.WithStatus(url.URLStatusSuspended),
		urlmock.WithShortCode("000abc"), urlmock.WithCustomAlias("my-alias"))
	cache.found["000abc"] = target(t, url.URLStatusSuspended, time.Now().Add(time.Hour))
	cache.found["my-alias"] = cache.found["000abc"]
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(suspended, nil)
	var saved url.URL
	expectUpdate(urls, &saved)

	err := svc.Reinstate(context.Background(), suspended.ID().String())

	require.NoError(t, err)
	require.Equal(t, url.URLStatusActive, saved.Status())
	require.Equal(t, []string{"url.reinstated"}, eventTypes(saved))
	require.Empty(t, cache.found)
}

func TestReinstate_OnlyASuspendedUrlCanBeReinstated(t *testing.T) {
	svc, urls, _ := newCachedService(t)
	active := cachedUrl(t, moderatorOwner)
	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(active, nil)

	err := svc.Reinstate(context.Background(), active.ID().String())

	require.True(t, errdefs.IsConflict(err), "got %v", err)
}

func TestReinstate_AMalformedOrUnknownIdIsNotFound(t *testing.T) {
	svc, urls, _ := newCachedService(t)

	require.True(t, errdefs.IsNotFound(svc.Reinstate(context.Background(), "not-a-uuid")))

	urls.EXPECT().FetchById(gomock.Any(), gomock.Any()).Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))
	require.True(t, errdefs.IsNotFound(svc.Reinstate(context.Background(), "018f4b0e-0000-7000-8000-000000000002")))
}
