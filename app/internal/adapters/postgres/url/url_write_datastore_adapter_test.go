package urldatastore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	mockpostgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	mockidentity "github.com/sanctumlabs/fupi/app/internal/domain/identity/mocks"
	domainurl "github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/fupi/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
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

// TestSoftDelete_MarksTheAggregateDeletedAndPersistsIt asserts the row is loaded with its keywords, transitioned through
// MarkDeleted and written exactly as Update writes it, with url.deleted in the outbox.
func (suite *UrlWriteDatastoreAdapterTestSuite) TestSoftDelete_MarksTheAggregateDeletedAndPersistsIt() {
	mockUrl, mockUrlErr := urlmock.MockUrl(urlmock.WithExpiresOn(time.Now().Add(time.Hour * 24)))
	suite.Require().NoError(mockUrlErr)

	urlID := pgtype.UUID{Bytes: mockUrl.ID(), Valid: true}
	row := mockpostgresql.MockUrl(mockpostgresql.WithUrl(*mockUrl))
	row.ID = urlID
	deletedRow := row
	deletedRow.Status = postgresql.UrlStatusDELETED

	gomock.InOrder(
		suite.mockUrlWriteQuerier.EXPECT().QueryUrlById(gomock.Any(), urlID).Return(postgresql.QueryUrlByIdRow{Url: row}, nil),
		suite.mockUrlWriteQuerier.EXPECT().
			QueryActiveKeywordsByUrlIds(gomock.Any(), []pgtype.UUID{urlID}).
			Return([]postgresql.QueryActiveKeywordsByUrlIdsRow{{Keyword: postgresql.Keyword{UrlID: urlID, Value: "one"}}}, nil),
		suite.mockUrlWriteQuerier.EXPECT().
			QueryUpdateUrl(gomock.Any(), gomock.Cond(func(p postgresql.QueryUpdateUrlParams) bool {
				return p.ID == urlID && p.Status == postgresql.UrlStatusDELETED
			})).
			Return(deletedRow, nil),
		suite.mockUrlWriteQuerier.EXPECT().QueryDeleteKeywordsByUrlId(gomock.Any(), urlID).Return(nil),
		suite.mockUrlWriteQuerier.EXPECT().QueryCreateKeyword(gomock.Any(), postgresql.QueryCreateKeywordParams{UrlID: urlID, Value: "one"}),
		suite.mockUrlWriteQuerier.EXPECT().
			QueryCreateOutboxEvent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error) {
				suite.Equal("url.deleted", params.EventType)
				suite.Equal("url.events", params.Destination)
				return postgresql.OutboxEvent{}, nil
			}),
	)

	suite.Require().NoError(suite.urlWriteDatastoreAdapter.SoftDelete(context.Background(), entity.IDToString(mockUrl.ID())))
}

func (suite *UrlWriteDatastoreAdapterTestSuite) TestSoftDelete_MissingUrlIsNotFound() {
	suite.mockUrlWriteQuerier.EXPECT().
		QueryUrlById(gomock.Any(), gomock.Any()).
		Return(postgresql.QueryUrlByIdRow{}, pgx.ErrNoRows)

	err := suite.urlWriteDatastoreAdapter.SoftDelete(context.Background(), entity.NewID().String())
	suite.True(errdefs.IsNotFound(err), "expected NotFound, got %v", err)
}

func (suite *UrlWriteDatastoreAdapterTestSuite) TestSoftDelete_MalformedIdIsNotFound() {
	err := suite.urlWriteDatastoreAdapter.SoftDelete(context.Background(), "not-a-uuid")
	suite.True(errdefs.IsNotFound(err), "expected NotFound, got %v", err)
}

func (suite *UrlWriteDatastoreAdapterTestSuite) TestSoftDelete_AlreadyDeletedIsConflictAndWritesNothing() {
	mockUrl, mockUrlErr := urlmock.MockUrl(urlmock.WithExpiresOn(time.Now().Add(time.Hour * 24)))
	suite.Require().NoError(mockUrlErr)
	row := mockpostgresql.MockUrl(mockpostgresql.WithUrl(*mockUrl), mockpostgresql.WithStatus(postgresql.UrlStatusDELETED))
	row.ID = pgtype.UUID{Bytes: mockUrl.ID(), Valid: true}

	suite.mockUrlWriteQuerier.EXPECT().QueryUrlById(gomock.Any(), gomock.Any()).Return(postgresql.QueryUrlByIdRow{Url: row}, nil)
	suite.mockUrlWriteQuerier.EXPECT().QueryActiveKeywordsByUrlIds(gomock.Any(), gomock.Any()).Return(nil, nil)

	err := suite.urlWriteDatastoreAdapter.SoftDelete(context.Background(), entity.IDToString(mockUrl.ID()))
	suite.True(errdefs.IsConflict(err), "expected Conflict, got %v", err)
}

// TestSave_WritesRecordedEventsToTheOutbox asserts URLCreated is written in the same transaction as the url.
func (suite *UrlWriteDatastoreAdapterTestSuite) TestSave_WritesRecordedEventsToTheOutbox() {
	shortCode, shortCodeErr := domainurl.NewShortCode("abc1234")
	suite.Require().NoError(shortCodeErr)
	created, createErr := domainurl.Create(domainurl.CreateURLParams{
		UserId:      entity.NewID().String(),
		ShortCode:   shortCode,
		OriginalUrl: "https://example.com/some/long/path",
		ExpiresOn:   time.Now().Add(time.Hour),
		Keywords:    []string{"docs"},
	})
	suite.Require().NoError(createErr)

	suite.mockUrlWriteQuerier.EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(mockpostgresql.MockUrl(mockpostgresql.WithUrl(*created)), nil).
		Times(1)
	suite.mockUrlWriteQuerier.EXPECT().
		QueryCreateKeyword(gomock.Any(), gomock.Any()).
		Times(1)
	suite.mockUrlWriteQuerier.EXPECT().
		QueryCreateOutboxEvent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error) {
			suite.Equal("url.created", params.EventType)
			suite.Equal("url.events", params.Destination)
			suite.Equal(entity.IDToString(created.ID()), params.PartitionKey.String)
			return postgresql.OutboxEvent{}, nil
		}).
		Times(1)

	actual, err := suite.urlWriteDatastoreAdapter.Save(context.Background(), *created)
	suite.Require().NoError(err)
	suite.Require().Len(actual.Keywords(), 1)
	suite.Equal("docs", actual.Keywords()[0].Value)
	suite.Empty(actual.DomainEvents(), "the returned url must not carry events that are already in the outbox")
}

// TestUpdate_PersistsStatusExpiryKeywordsAndEvents asserts a deleted url is written with its status, its keywords are
// replaced and URLDeleted reaches the outbox, all through the one querier the transaction is bound to.
func (suite *UrlWriteDatastoreAdapterTestSuite) TestUpdate_PersistsStatusExpiryKeywordsAndEvents() {
	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithKeywords([]string{"one", "two"}),
	)
	suite.Require().NoError(mockUrlErr)
	suite.Require().NoError(mockUrl.MarkDeleted())

	urlID := pgtype.UUID{Bytes: mockUrl.ID(), Valid: true}
	updatedRow := mockpostgresql.MockUrl(mockpostgresql.WithUrl(*mockUrl), mockpostgresql.WithStatus(postgresql.UrlStatusDELETED))
	updatedRow.ID = urlID

	gomock.InOrder(
		suite.mockUrlWriteQuerier.EXPECT().
			QueryUpdateUrl(gomock.Any(), postgresql.QueryUpdateUrlParams{
				ID:        urlID,
				Status:    postgresql.UrlStatusDELETED,
				ExpiresOn: pgtype.Timestamptz{Time: mockUrl.ExpiresOn(), Valid: true},
			}).
			Return(updatedRow, nil),
		suite.mockUrlWriteQuerier.EXPECT().QueryDeleteKeywordsByUrlId(gomock.Any(), urlID).Return(nil),
		suite.mockUrlWriteQuerier.EXPECT().QueryCreateKeyword(gomock.Any(), postgresql.QueryCreateKeywordParams{UrlID: urlID, Value: "one"}),
		suite.mockUrlWriteQuerier.EXPECT().QueryCreateKeyword(gomock.Any(), postgresql.QueryCreateKeywordParams{UrlID: urlID, Value: "two"}),
		suite.mockUrlWriteQuerier.EXPECT().
			QueryCreateOutboxEvent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error) {
				suite.Equal("url.deleted", params.EventType)
				return postgresql.OutboxEvent{}, nil
			}),
	)

	actual, err := suite.urlWriteDatastoreAdapter.Update(context.Background(), *mockUrl)
	suite.Require().NoError(err)
	suite.Equal(domainurl.URLStatusDeleted, actual.Status())
	suite.Len(actual.Keywords(), 2)
}

func (suite *UrlWriteDatastoreAdapterTestSuite) TestUpdate_MissingUrlIsNotFound() {
	mockUrl, mockUrlErr := urlmock.MockUrl(urlmock.WithExpiresOn(time.Now().Add(time.Hour * 24)))
	suite.Require().NoError(mockUrlErr)

	suite.mockUrlWriteQuerier.EXPECT().
		QueryUpdateUrl(gomock.Any(), gomock.Any()).
		Return(postgresql.Url{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.urlWriteDatastoreAdapter.Update(context.Background(), *mockUrl)
	suite.True(errdefs.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestSave_MapsOriginalUrlUniqueViolationToConflict covers the global uniqueness rule on
// urls.original_url: the partial unique index raises SQLSTATE 23505, and callers need to tell "this
// target is already shortened" apart from a genuine write failure.
func (suite *UrlWriteDatastoreAdapterTestSuite) TestSave_MapsOriginalUrlUniqueViolationToConflict() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	mockUrl, mockUrlErr := urlmock.MockUrl()
	suite.Require().NoError(mockUrlErr)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(postgresql.Url{}, &pgconn.PgError{
			Code:           "23505",
			ConstraintName: "idx_urls_original_url",
			Message:        `duplicate key value violates unique constraint "idx_urls_original_url"`,
		}).
		Times(1)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsConflict(actualErr), "expected a Conflict error, got %v", actualErr)
	suite.ErrorIs(actualErr, errdefs.ErrURLAlreadyExists)
	suite.Empty(actual)
}

// TestSave_MapsShortCodeUniqueViolationToConflictWithoutUrlExistsError guards against reporting a
// short code collision as "this url is already shortened": they are different failures.
func (suite *UrlWriteDatastoreAdapterTestSuite) TestSave_MapsShortCodeUniqueViolationToConflictWithoutUrlExistsError() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	mockUrl, mockUrlErr := urlmock.MockUrl()
	suite.Require().NoError(mockUrlErr)

	suite.mockUrlWriteQuerier.
		EXPECT().
		QueryCreateUrl(gomock.Any(), gomock.Any()).
		Return(postgresql.Url{}, &pgconn.PgError{
			Code:           "23505",
			ConstraintName: "idx_urls_short_code",
			Message:        `duplicate key value violates unique constraint "idx_urls_short_code"`,
		}).
		Times(1)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Require().Error(actualErr)
	suite.True(errdefs.IsConflict(actualErr), "expected a Conflict error, got %v", actualErr)
	suite.NotErrorIs(actualErr, errdefs.ErrURLAlreadyExists)
	suite.Empty(actual)
}
