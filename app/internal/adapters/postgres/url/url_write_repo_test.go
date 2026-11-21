package urlrepo

import (
	"context"
	"fmt"
	"testing"
	"time"

	mockpostgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/mocks"
	mockpostgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/curtz/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/curtz/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UrlWriteRepoAdapterTestSuite struct {
	suite.Suite
	mockCtrl            *gomock.Controller
	mockDbClient        *mockdatabase.MockPostgresDatabaseClient
	mockUrlWriteQuerier *mockpostgresrepo.MockUrlWriteQuerier
	mockUserReadRepo    *mockidentity.MockUserReadRepository
	urlWriteRepoAdapter *urlWriteRepositoryAdapter
	config              database.Config
}

func (suite *UrlWriteRepoAdapterTestSuite) SetupTest() {
	config := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	mockCtrl := gomock.NewController(suite.T())
	suite.mockCtrl = mockCtrl
	suite.mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
	suite.mockUrlWriteQuerier = mockpostgresrepo.NewMockUrlWriteQuerier(mockCtrl)
	suite.mockUserReadRepo = mockidentity.NewMockUserReadRepository(mockCtrl)
	suite.urlWriteRepoAdapter = &urlWriteRepositoryAdapter{
		logPrefix: "UrlWriteRepoAdapter",
		dbClient:  suite.mockDbClient,
		userRepo:  suite.mockUserReadRepo,
		config:    config,
	}
	suite.config = config

	injectMockUrlWriteTx(suite.urlWriteRepoAdapter, suite.mockUrlWriteQuerier)
}

func TestUrlWriteRepoAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(UrlWriteRepoAdapterTestSuite))
}

func (suite *UrlWriteRepoAdapterTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// TestCreate_CreatesNewUrlSuccessfully tests the Create method of the UrlWriteRepositoryAdapter
func (suite *UrlWriteRepoAdapterTestSuite) TestCreate_CreatesNewUrlSuccessfully() {
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
	suite.NoError(mockUrlErr)

	mockCreatedUrl := mockpostgresql.MockUrl(
		mockpostgresql.WithUrl(*mockUrl),
	)

	suite.mockUserReadRepo.
		EXPECT().
		FetchById(gomock.Any(), gomock.Any()).
		Return(*mockUser, nil).
		Times(1)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryUrlStatusByName(gomock.Any(), gomock.Any()).
		Return(mockpostgresql.MockUrlStatus(url.URLStatusActive), nil).
		Times(1)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(mockCreatedUrl, nil).
		Times(1)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateKeyword(gomock.Any(), gomock.Any()).
		AnyTimes()

	actual, actualErr := suite.urlWriteRepoAdapter.Create(ctx, *mockUrl)
	suite.Nil(actualErr)
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

// TestCreate_FailsWhenUserDoesNotExist tests the Create method of the UrlWriteRepositoryAdapter fails when the user does not exist in the database
func (suite *UrlWriteRepoAdapterTestSuite) TestCreate_FailsWhenUserDoesNotExist() {
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
	suite.NoError(mockUrlErr)

	mockCreatedUrl := mockpostgresql.MockUrl(
		mockpostgresql.WithUrl(*mockUrl),
	)

	userErr := fmt.Errorf("user not found")

	suite.mockUserReadRepo.
		EXPECT().
		FetchById(gomock.Any(), gomock.Any()).
		Return(identity.User{}, userErr).
		Times(1)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryUrlStatusByName(gomock.Any(), gomock.Any()).
		Return(mockpostgresql.MockUrlStatus(url.URLStatusActive), nil).
		Times(0)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(mockCreatedUrl, nil).
		Times(0)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateKeyword(gomock.Any(), gomock.Any()).
		Times(0)

	actual, actualErr := suite.urlWriteRepoAdapter.Create(ctx, *mockUrl)
	suite.NotNil(actualErr)
	suite.Empty(actual)
}
