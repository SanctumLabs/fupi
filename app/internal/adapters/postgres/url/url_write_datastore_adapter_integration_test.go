//go:build integration

package urldatastore

import (
	"context"
	"testing"
	"time"

	identitydatastore "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/identity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	domainurl "github.com/sanctumlabs/curtz/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/curtz/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/sanctumlabs/curtz/app/test"
	"github.com/stretchr/testify/suite"
)

type UrlWriteDatastoreAdapterIntegrationTestSuite struct {
	suite.Suite
	urlWriteDatastoreAdapter   domainurl.UrlWriteDatastore
	userWriteDatastore         identity.UserWriteDatastore
	config                     database.Config
	testPostgresDatabaseClient database.PostgresDatabaseClient
}

func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) SetupTest() {
	ctx := context.Background()
	testPostgresDatabaseClient := test.TestPostgresDatabaseClientHelper(suite.T(), ctx)

	config := database.Config{
		OperationTimeout: 5 * time.Minute,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	suite.testPostgresDatabaseClient = testPostgresDatabaseClient
	suite.userWriteDatastore = identitydatastore.NewUserWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.urlWriteDatastoreAdapter = NewUrlWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.config = config
}

func TestUrlWriteDatastoreAdapterIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(UrlWriteDatastoreAdapterIntegrationTestSuite))
}

func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) AfterTest(_, _ string) {
	suite.testPostgresDatabaseClient.Close()
}

// TestCreate_CreatesNewUrlSuccessfully tests the Create method of the UrlWriteDatastoreAdapter
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestCreate_CreatesNewUrlSuccessfully() {
	bcgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bcgCtx, suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	// urls.user_id references users(id), so the owner must exist first
	_, saveUserErr := suite.userWriteDatastore.Save(ctx, *mockUser)
	suite.Require().NoError(saveUserErr)

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithCustomAlias("custom"),
		urlmock.WithShortCode("shortcode"),
	)

	// Require stops the test immediately on failure, preventing a nil
	// dereference on *mockUrl in the Create call below.
	suite.Require().NoError(mockUrlErr)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Require().NoError(actualErr)

	suite.Equal(mockUrl.UserId(), actual.UserId())
	suite.Equal(mockUrl.ShortCode(), actual.ShortCode())
	suite.Equal(mockUrl.CustomAlias(), actual.CustomAlias())
	suite.Equal(mockUrl.OriginalURL(), actual.OriginalURL())
	// Postgres stores microseconds and drops the monotonic clock reading
	suite.WithinDuration(mockUrl.ExpiresOn(), actual.ExpiresOn(), time.Microsecond)
	suite.Equal(mockUrl.Status(), actual.Status())
	suite.Equal(mockUrl.OgTitle(), actual.OgTitle())
	suite.Equal(mockUrl.OgDescription(), actual.OgDescription())
	suite.Equal(mockUrl.OgImageUrl(), actual.OgImageUrl())
}

// TestSave_FailsWhenUserDoesNotExist tests that the users foreign key rejects a URL whose owner
// was never saved
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_FailsWhenUserDoesNotExist() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithShortCode("shortcode"),
	)
	suite.Require().NoError(mockUrlErr)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Error(actualErr)
	suite.Empty(actual)
}
