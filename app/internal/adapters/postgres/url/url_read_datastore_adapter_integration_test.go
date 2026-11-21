//go:build integration

package urldatastore

import (
	"context"
	"testing"
	"time"

	identitydatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/identity"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/fupi/app/internal/domain/identity/mocks"
	domainurl "github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/suite"
)

type UrlReadDatastoreAdapterIntegrationTestSuite struct {
	suite.Suite
	urlReadDatastoreAdapter    domainurl.UrlReadDatastore
	urlWriteDatastoreAdapter   domainurl.UrlWriteDatastore
	userWriteDatastore         identity.UserWriteDatastore
	config                     database.Config
	testPostgresDatabaseClient database.PostgresDatabaseClient
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) SetupTest() {
	ctx := context.Background()
	testPostgresDatabaseClient := test.TestPostgresDatabaseClientHelper(suite.T(), ctx)

	config := database.Config{
		OperationTimeout: 5 * time.Minute,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	suite.testPostgresDatabaseClient = testPostgresDatabaseClient
	suite.userWriteDatastore = identitydatastore.NewUserWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.urlWriteDatastoreAdapter = NewUrlWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.urlReadDatastoreAdapter = NewUrlReadDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.config = config
}

func TestUrlReadDatastoreAdapterIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(UrlReadDatastoreAdapterIntegrationTestSuite))
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) AfterTest(_, _ string) {
	suite.testPostgresDatabaseClient.Close()
}

// seedUrl persists a user and then a URL owned by that user, returning the stored URL.
// urls.user_id references users(id), so the owner has to exist before the URL is written.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) seedUrl(ctx context.Context, options ...urlmock.MockUrlOption) domainurl.URL {
	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	_, saveUserErr := suite.userWriteDatastore.Save(ctx, *mockUser)
	suite.Require().NoError(saveUserErr)

	defaults := []urlmock.MockUrlOption{
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour)),
	}

	mockUrl, mockUrlErr := urlmock.MockUrl(append(defaults, options...)...)
	suite.Require().NoError(mockUrlErr)

	savedUrl, savedUrlErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Require().NoError(savedUrlErr)

	return savedUrl
}

// seedUrlsFor persists count URLs owned by one new user, returning the owner's id.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) seedUrlsFor(ctx context.Context, count int, options ...urlmock.MockUrlOption) string {
	owner, ownerErr := mockidentity.MockUser()
	suite.Require().NoError(ownerErr)
	_, saveOwnerErr := suite.userWriteDatastore.Save(ctx, *owner)
	suite.Require().NoError(saveOwnerErr)

	for range count {
		defaults := []urlmock.MockUrlOption{
			urlmock.WithUserId(owner.ID().String()),
			urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour)),
		}
		mockUrl, mockUrlErr := urlmock.MockUrl(append(defaults, options...)...)
		suite.Require().NoError(mockUrlErr)
		_, saveErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
		suite.Require().NoError(saveErr)
	}

	return owner.ID().String()
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) newContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), suite.config.OperationTimeout)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchById_ReturnsStoredUrl() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(actualErr)
	suite.Equal(saved.ID(), actual.ID())
	suite.Equal(saved.UserId(), actual.UserId())
	suite.Equal(saved.ShortCode(), actual.ShortCode())
	suite.Equal(saved.CustomAlias(), actual.CustomAlias())
	suite.Equal(saved.OriginalURL(), actual.OriginalURL())
	suite.Equal(domainurl.URLStatusActive, actual.Status())
	suite.WithinDuration(saved.ExpiresOn(), actual.ExpiresOn(), time.Microsecond)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchById_NotFoundForUnknownId() {
	ctx, cancel := suite.newContext()
	defer cancel()

	_, actualErr := suite.urlReadDatastoreAdapter.FetchById(ctx, entity.NewID().String())
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsNotFound(actualErr), "expected a NotFound error, got %v", actualErr)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByShortCode_ReturnsStoredUrl() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx, urlmock.WithShortCode("abc1234"))

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByShortCode(ctx, "abc1234")
	suite.Require().NoError(actualErr)
	suite.Equal(saved.ID(), actual.ID())
	suite.Equal("abc1234", actual.ShortCode().Value())
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByShortCode_NotFound() {
	ctx, cancel := suite.newContext()
	defer cancel()

	_, actualErr := suite.urlReadDatastoreAdapter.FetchByShortCode(ctx, "nosuch")
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsNotFound(actualErr), "expected a NotFound error, got %v", actualErr)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByCustomAlias_ReturnsStoredUrl() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx, urlmock.WithCustomAlias("my-alias"))

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByCustomAlias(ctx, "my-alias")
	suite.Require().NoError(actualErr)
	suite.Equal(saved.ID(), actual.ID())
	suite.Equal("my-alias", actual.CustomAlias().Value())
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByCustomAlias_NotFound() {
	ctx, cancel := suite.newContext()
	defer cancel()

	_, actualErr := suite.urlReadDatastoreAdapter.FetchByCustomAlias(ctx, "ghost")
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsNotFound(actualErr), "expected a NotFound error, got %v", actualErr)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByOriginalUrl_ReturnsStoredUrl() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx, urlmock.WithOriginalUrl("https://example.com/original"))

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(ctx, "https://example.com/original")
	suite.Require().NoError(actualErr)
	suite.Equal(saved.ID(), actual.ID())
	suite.Equal("https://example.com/original", actual.OriginalURL().Value())
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByOriginalUrl_NotFound() {
	ctx, cancel := suite.newContext()
	defer cancel()

	_, actualErr := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(ctx, "https://example.com/never-stored")
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsNotFound(actualErr), "expected a NotFound error, got %v", actualErr)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchAll_ReturnsStoredUrlsWithTotal() {
	ctx, cancel := suite.newContext()
	defer cancel()

	first := suite.seedUrl(ctx)
	second := suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchAll(ctx, common.NewRequestParams())
	suite.Require().NoError(actualErr)
	suite.Equal(2, actual.Total)
	suite.Equal(2, actual.Size)
	suite.Equal(1, actual.Page)

	ids := make([]string, 0, len(actual.Records))
	for _, record := range actual.Records {
		ids = append(ids, record.ID().String())
	}
	suite.Contains(ids, first.ID().String())
	suite.Contains(ids, second.ID().String())
}

// TestFetchAll_PaginatesWithTotal checks that the total reflects every matching row while the page
// only carries the requested window.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchAll_PaginatesWithTotal() {
	ctx, cancel := suite.newContext()
	defer cancel()

	for range 3 {
		suite.seedUrl(ctx)
	}

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchAll(ctx,
		common.NewRequestParams(common.WithRequestLimit(2), common.WithOffset(2)))
	suite.Require().NoError(actualErr)
	suite.Equal(3, actual.Total)
	suite.Equal(1, actual.Size)
	suite.Equal(2, actual.Page)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_ReturnsOnlyThatUsersUrls() {
	ctx, cancel := suite.newContext()
	defer cancel()

	mine := suite.seedUrl(ctx)
	// A second seeded URL belongs to a different user, so it must not appear in the result.
	suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, mine.UserId().String(), common.NewRequestParams())
	suite.Require().NoError(actualErr)
	suite.Equal(1, actual.Total)
	suite.Require().Len(actual.Records, 1)
	suite.Equal(mine.ID(), actual.Records[0].ID())
	suite.Equal(mine.UserId(), actual.Records[0].UserId())
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_EmptyForUnknownUser() {
	ctx, cancel := suite.newContext()
	defer cancel()

	suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, entity.NewID().String(), common.NewRequestParams())
	suite.Require().NoError(actualErr)
	suite.Equal(0, actual.Total)
	suite.Empty(actual.Records)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByStatus_ReturnsActiveUrls() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByStatus(ctx, domainurl.URLStatusActive, common.NewRequestParams())
	suite.Require().NoError(actualErr)
	suite.Equal(1, actual.Total)
	suite.Require().Len(actual.Records, 1)
	suite.Equal(saved.ID(), actual.Records[0].ID())
	suite.Equal(domainurl.URLStatusActive, actual.Records[0].Status())
}

// TestFetchByStatus_EmptyWhenNoneMatch guards the status filter: an ACTIVE row must not leak into a
// query for another status.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByStatus_EmptyWhenNoneMatch() {
	ctx, cancel := suite.newContext()
	defer cancel()

	suite.seedUrl(ctx)

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByStatus(ctx, domainurl.URLStatusSuspended, common.NewRequestParams())
	suite.Require().NoError(actualErr)
	suite.Equal(0, actual.Total)
	suite.Empty(actual.Records)
}

// TestFetchExpiredActive_ReturnsOnlyAlreadyExpiredUrls is the query the expiry job depends on: an
// ACTIVE URL past its expiry is returned, one still in the future is not.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchExpiredActive_ReturnsOnlyAlreadyExpiredUrls() {
	ctx, cancel := suite.newContext()
	defer cancel()

	expired := suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(-1*time.Hour)))
	suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(24*time.Hour)))

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchExpiredActive(ctx, time.Now(), 10)
	suite.Require().NoError(actualErr)
	suite.Require().Len(actual, 1)
	suite.Equal(expired.ID(), actual[0].ID())
	suite.Equal(domainurl.URLStatusActive, actual[0].Status())
}

// TestFetchExpiredActive_RespectsLimit matters because the expiry job drains the backlog in batches.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchExpiredActive_RespectsLimit() {
	ctx, cancel := suite.newContext()
	defer cancel()

	for i := range 3 {
		suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(-time.Duration(i+1)*time.Hour)))
	}

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchExpiredActive(ctx, time.Now(), 2)
	suite.Require().NoError(actualErr)
	suite.Len(actual, 2)
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchExpiredActive_EmptyWhenNothingExpired() {
	ctx, cancel := suite.newContext()
	defer cancel()

	suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(24*time.Hour)))

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchExpiredActive(ctx, time.Now(), 10)
	suite.Require().NoError(actualErr)
	suite.Empty(actual)
}

// TestCountExpiredActive_CountsAllExpiredUrlsNotJustABatch is the expiry job's backlog: every ACTIVE URL past its
// expiry counts, whatever the batch size, and one still in the future does not.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestCountExpiredActive_CountsAllExpiredUrlsNotJustABatch() {
	ctx, cancel := suite.newContext()
	defer cancel()

	for i := range 3 {
		suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(-time.Duration(i+1)*time.Hour)))
	}
	suite.seedUrl(ctx, urlmock.WithExpiresOn(time.Now().Add(24*time.Hour)))

	actual, actualErr := suite.urlReadDatastoreAdapter.CountExpiredActive(ctx, time.Now())
	suite.Require().NoError(actualErr)
	suite.Equal(int64(3), actual)
}

// TestFetchByOriginalUrl_FindsRespelledTarget closes the loop on normalization: the row is stored in
// canonical form, and a lookup spelled differently must still find it.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByOriginalUrl_FindsRespelledTarget() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx, urlmock.WithOriginalUrl("https://example.com/canonical"))

	for _, respelling := range []string{
		"https://example.com/canonical",
		"https://EXAMPLE.COM/canonical",
		"https://example.com:443/canonical",
		"https://example.com/canonical/",
		"https://example.com/canonical?utm_source=news",
		"HTTPS://Example.Com:443/canonical/?utm_medium=email",
	} {
		suite.Run(respelling, func() {
			actual, actualErr := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(ctx, respelling)
			suite.Require().NoError(actualErr, "%q should have found the stored target", respelling)
			suite.Equal(saved.ID(), actual.ID())
		})
	}
}

// TestFetchByUserId_PagesThroughEveryUrl checks the second page of a user's 150 urls carries the remaining 50 and the
// total still counts all 150.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_PagesThroughEveryUrl() {
	ctx, cancel := suite.newContext()
	defer cancel()

	owner := suite.seedUrlsFor(ctx, 150)

	first, firstErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner,
		common.NewRequestParams(common.WithRequestLimit(100), common.WithOffset(0)))
	suite.Require().NoError(firstErr)
	suite.Equal(150, first.Total)
	suite.Equal(100, first.Size)
	suite.Equal(1, first.Page)

	second, secondErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner,
		common.NewRequestParams(common.WithRequestLimit(100), common.WithOffset(100)))
	suite.Require().NoError(secondErr)
	suite.Equal(150, second.Total)
	suite.Equal(50, second.Size)
	suite.Equal(2, second.Page)

	seen := make(map[string]struct{}, 150)
	for _, record := range append(first.Records, second.Records...) {
		seen[record.ID().String()] = struct{}{}
	}
	suite.Len(seen, 150, "the two pages must not overlap")
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_OrdersByCreatedAt() {
	ctx, cancel := suite.newContext()
	defer cancel()

	owner := suite.seedUrlsFor(ctx, 3)

	for _, order := range []common.SortOrder{common.SortOderAsc, common.SortOrderDesc} {
		actual, actualErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner, common.NewRequestParams(common.WithSortOrder(order)))
		suite.Require().NoError(actualErr)
		suite.Require().Len(actual.Records, 3)
		for i := 1; i < len(actual.Records); i++ {
			earlier, later := actual.Records[i-1].CreatedAt(), actual.Records[i].CreatedAt()
			if order == common.SortOderAsc {
				suite.False(earlier.After(later), "ASC: %v should not be after %v", earlier, later)
			} else {
				suite.False(earlier.Before(later), "DESC: %v should not be before %v", earlier, later)
			}
		}
	}
}

func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_FiltersByStatus() {
	ctx, cancel := suite.newContext()
	defer cancel()

	owner := suite.seedUrlsFor(ctx, 2)

	expired, expiredErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner,
		common.NewRequestParams(common.WithStatus(string(domainurl.URLStatusExpired))))
	suite.Require().NoError(expiredErr)
	suite.Equal(0, expired.Total)

	active, activeErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner,
		common.NewRequestParams(common.WithStatus(string(domainurl.URLStatusActive))))
	suite.Require().NoError(activeErr)
	suite.Equal(2, active.Total)
}

// TestFetchByUserId_NeverReturnsDeletedUrls asserts a soft-deleted url stays hidden even when the params ask for it.
func (suite *UrlReadDatastoreAdapterIntegrationTestSuite) TestFetchByUserId_NeverReturnsDeletedUrls() {
	ctx, cancel := suite.newContext()
	defer cancel()

	saved := suite.seedUrl(ctx)
	suite.Require().NoError(saved.MarkDeleted())
	_, updateErr := suite.urlWriteDatastoreAdapter.Update(ctx, saved)
	suite.Require().NoError(updateErr)
	owner := saved.UserId().String()

	actual, actualErr := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, owner, common.NewRequestParams(common.WithIncludeDeleted(true)))
	suite.Require().NoError(actualErr)
	suite.Equal(0, actual.Total)
	suite.Empty(actual.Records)
}
