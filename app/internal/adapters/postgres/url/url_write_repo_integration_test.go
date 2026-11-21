package urlrepo

import (
	"context"
	"testing"
	"time"

	identityrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/identity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	domainurl "github.com/sanctumlabs/curtz/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/curtz/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/sanctumlabs/curtz/app/test"
	"github.com/stretchr/testify/suite"
)

type UrlWriteRepoAdapterIntegrationTestSuite struct {
	suite.Suite
	urlWriteRepoAdapter        domainurl.UrlWriteRepository
	userReadRepoAdapter        identity.UserReadRepository
	config                     database.Config
	testPostgresDatabaseClient database.PostgresDatabaseClient
}

func (suite *UrlWriteRepoAdapterIntegrationTestSuite) SetupTest() {
	ctx := context.Background()
	testPostgresDatabaseClient := test.TestPostgresDatabaseClient(suite.T(), ctx)

	config := database.Config{
		OperationTimeout: 5 * time.Minute,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	suite.testPostgresDatabaseClient = testPostgresDatabaseClient
	userReadRepoAdapter := identityrepo.NewUserReadRepoAdapter(testPostgresDatabaseClient, config)
	urlWriteRepoAdapter := NewUrlWriteRepoAdapter(
		testPostgresDatabaseClient, userReadRepoAdapter, config,
	)
	suite.urlWriteRepoAdapter = urlWriteRepoAdapter
	suite.config = config
}

func TestUrlWriteRepoAdapterIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(UrlWriteRepoAdapterIntegrationTestSuite))
}

func (suite *UrlWriteRepoAdapterIntegrationTestSuite) AfterTest(_, _ string) {
	suite.testPostgresDatabaseClient.Close()
}

// TestCreate_CreatesNewUrlSuccessfully tests the Create method of the UrlWriteRepositoryAdapter
func (suite *UrlWriteRepoAdapterIntegrationTestSuite) TestCreate_CreatesNewUrlSuccessfully() {
	bcgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bcgCtx, suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.NoError(mockUserErr)

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithCustomAlias("custom"),
		urlmock.WithShortCode("shortcode"),
	)

	// Require stops the test immediately on failure, preventing a nil
	// dereference on *mockUrl in the Create call below.
	suite.Require().NoError(mockUrlErr)

	actual, actualErr := suite.urlWriteRepoAdapter.Create(ctx, *mockUrl)
	suite.Require().NoError(actualErr)

	suite.Equal(mockUrl.UserId(), actual.UserId())
	suite.Equal(mockUrl.ShortCode(), actual.ShortCode())
	suite.Equal(mockUrl.CustomAlias(), actual.CustomAlias())
	suite.Equal(mockUrl.OriginalURL(), actual.OriginalURL())
	suite.Equal(mockUrl.ExpiresOn(), actual.ExpiresOn())
	suite.Equal(mockUrl.Status(), actual.Status())
	suite.Equal(mockUrl.OgTitle(), actual.OgTitle())
	suite.Equal(mockUrl.OgDescription(), actual.OgDescription())
	suite.Equal(mockUrl.OgImageUrl(), actual.OgImageUrl())
}
