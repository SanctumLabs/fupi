package urldatastore

import (
	"context"
	"fmt"
	"testing"
	"time"

	mockpostgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql/mocks"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	urlmock "github.com/sanctumlabs/curtz/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/curtz/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UrlWriteDatastoreAdapterTestSuite struct {
	suite.Suite
	mockCtrl                 *gomock.Controller
	mockDbClient             *mockdatabase.MockPostgresDatabaseClient
	mockUrlWriteQuerier      *mockpostgresrepo.MockUrlWriteQuerier
	urlWriteDatastoreAdapter *urlWriteDatastoreAdapter
	config                   database.Config
}

func (suite *UrlWriteDatastoreAdapterTestSuite) SetupTest() {
	config := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	mockCtrl := gomock.NewController(suite.T())
	suite.mockCtrl = mockCtrl
	suite.mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
	suite.mockUrlWriteQuerier = mockpostgresrepo.NewMockUrlWriteQuerier(mockCtrl)
	suite.urlWriteDatastoreAdapter = &urlWriteDatastoreAdapter{
		logPrefix: "UrlWriteDatastoreAdapter",
		dbClient:  suite.mockDbClient,
		config:    config,
	}
	suite.config = config

	injectMockUrlWriteTx(suite.urlWriteDatastoreAdapter, suite.mockUrlWriteQuerier)
}

func TestUrlWriteDatastoreAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(UrlWriteDatastoreAdapterTestSuite))
}

func (suite *UrlWriteDatastoreAdapterTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// TestCreate_CreatesNewUrlSuccessfully tests the Create method of the UrlWriteDatastoreAdapter
func (suite *UrlWriteDatastoreAdapterTestSuite) TestCreate_CreatesNewUrlSuccessfully() {
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

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(mockCreatedUrl, nil).
		Times(1)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateKeyword(gomock.Any(), gomock.Any()).
		AnyTimes()

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
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

// TestCreate_FailsWhenKeywordCannotBeSaved tests that a keyword that fails to save fails the whole
// Save rather than being silently dropped (ADR-0003)
func (suite *UrlWriteDatastoreAdapterTestSuite) TestCreate_FailsWhenKeywordCannotBeSaved() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
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

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(mockpostgresql.MockUrl(mockpostgresql.WithUrl(*mockUrl)), nil).
		Times(1)

	keywordErr := fmt.Errorf("keyword insert failed")
	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateKeyword(gomock.Any(), gomock.Any()).
		Return(postgresql.Keyword{}, keywordErr).
		Times(1)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.ErrorIs(actualErr, keywordErr)
	suite.Empty(actual)
}

// TestUnimplementedWrites_ReportNotImplemented tests that writes which are not built yet fail
// loudly instead of reporting success or panicking
func (suite *UrlWriteDatastoreAdapterTestSuite) TestUnimplementedWrites_ReportNotImplemented() {
	ctx := context.Background()

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithCustomAlias("custom"),
		urlmock.WithShortCode("shortcode"),
	)
	suite.Require().NoError(mockUrlErr)

	_, updateErr := suite.urlWriteDatastoreAdapter.Update(ctx, *mockUrl)
	suite.True(errdefs.IsNotImplemented(updateErr), "expected NotImplemented, got %v", updateErr)
	suite.True(errdefs.IsNotImplemented(suite.urlWriteDatastoreAdapter.SoftDelete(ctx, mockUrl.ID().String())))
	suite.True(errdefs.IsNotImplemented(suite.urlWriteDatastoreAdapter.Delete(ctx, mockUrl.ID().String())))
}
