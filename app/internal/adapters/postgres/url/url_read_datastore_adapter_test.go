package urldatastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	mockpostgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	domainurl "github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/fupi/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UrlReadDatastoreAdapterTestSuite struct {
	suite.Suite
	mockCtrl                *gomock.Controller
	mockDbClient            *mockdatabase.MockPostgresDatabaseClient
	mockUrlReadQuerier      *mockpostgresrepo.MockUrlReadQuerier
	urlReadDatastoreAdapter *urlReadDatastoreAdapter
	config                  database.Config
}

func (suite *UrlReadDatastoreAdapterTestSuite) SetupTest() {
	config := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	mockCtrl := gomock.NewController(suite.T())
	suite.mockCtrl = mockCtrl
	suite.mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
	suite.mockUrlReadQuerier = mockpostgresrepo.NewMockUrlReadQuerier(mockCtrl)
	suite.urlReadDatastoreAdapter = &urlReadDatastoreAdapter{
		logPrefix: "UrlReadDatastoreAdapter",
		dbClient:  suite.mockDbClient,
		config:    config,
	}
	suite.config = config

	injectMockUrlReadTx(suite.urlReadDatastoreAdapter, suite.mockUrlReadQuerier)
}

func TestUrlReadDatastoreAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(UrlReadDatastoreAdapterTestSuite))
}

func (suite *UrlReadDatastoreAdapterTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// newMockUrlRow builds a URL entity together with the sqlc row a query would return for it.
// mockpostgresql.WithUrl does not carry the entity id across, so it is set explicitly here to keep
// the two representations referring to the same URL.
func (suite *UrlReadDatastoreAdapterTestSuite) newMockUrlRow(options ...urlmock.MockUrlOption) (*domainurl.URL, postgresql.Url) {
	defaults := []urlmock.MockUrlOption{urlmock.WithId(entity.NewID())}

	mockUrl, mockUrlErr := urlmock.MockUrl(append(defaults, options...)...)
	suite.Require().NoError(mockUrlErr)

	urlRecord := mockpostgresql.MockUrl(mockpostgresql.WithUrl(*mockUrl))
	urlRecord.ID = pgtype.UUID{Bytes: mockUrl.ID(), Valid: true}

	return mockUrl, urlRecord
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchById_Success() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow()

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlById(gomock.Any(), pgtype.UUID{Bytes: mockUrl.ID(), Valid: true}).
		Return(postgresql.QueryUrlByIdRow{Url: urlRecord}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchById(ctx, mockUrl.ID().String())
	suite.NoError(err)
	suite.Equal(mockUrl.ID(), actual.ID())
	suite.Equal(mockUrl.UserId(), actual.UserId())
	suite.Equal(mockUrl.ShortCode(), actual.ShortCode())
	suite.Equal(mockUrl.OriginalURL(), actual.OriginalURL())
	suite.Equal(domainurl.URLStatusActive, actual.Status())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchById_NotFound() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlById(gomock.Any(), gomock.Any()).
		Return(postgresql.QueryUrlByIdRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.urlReadDatastoreAdapter.FetchById(context.Background(), entity.NewID().String())
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

// TestFetchById_InvalidId asserts a malformed id never reaches the database.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchById_InvalidId() {
	_, err := suite.urlReadDatastoreAdapter.FetchById(context.Background(), "not-a-uuid")
	suite.Error(err)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByShortCode_Success() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow(urlmock.WithShortCode("abc1234"))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByUrlShortCode(gomock.Any(), "abc1234").
		Return(postgresql.QueryUrlByUrlShortCodeRow{Url: urlRecord}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByShortCode(ctx, "abc1234")
	suite.NoError(err)
	suite.Equal(mockUrl.ID(), actual.ID())
	suite.Equal("abc1234", actual.ShortCode().Value())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByShortCode_NotFound() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByUrlShortCode(gomock.Any(), "missing").
		Return(postgresql.QueryUrlByUrlShortCodeRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.urlReadDatastoreAdapter.FetchByShortCode(context.Background(), "missing")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByCustomAlias_Success() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow(urlmock.WithCustomAlias("alias"))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByCustomAlias(gomock.Any(), pgtype.Text{String: "alias", Valid: true}).
		Return(postgresql.QueryUrlByCustomAliasRow{Url: urlRecord}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByCustomAlias(ctx, "alias")
	suite.NoError(err)
	suite.Equal(mockUrl.ID(), actual.ID())
	suite.Equal("alias", actual.CustomAlias().Value())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByCustomAlias_NotFound() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByCustomAlias(gomock.Any(), pgtype.Text{String: "nope", Valid: true}).
		Return(postgresql.QueryUrlByCustomAliasRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.urlReadDatastoreAdapter.FetchByCustomAlias(context.Background(), "nope")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByOriginalUrl_Success() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow(urlmock.WithOriginalUrl("https://example.com/target"))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByOriginalUrl(gomock.Any(), "https://example.com/target").
		Return(postgresql.QueryUrlByOriginalUrlRow{Url: urlRecord}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(ctx, "https://example.com/target")
	suite.NoError(err)
	suite.Equal(mockUrl.ID(), actual.ID())
	suite.Equal("https://example.com/target", actual.OriginalURL().Value())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByOriginalUrl_NotFound() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByOriginalUrl(gomock.Any(), "https://example.com/missing").
		Return(postgresql.QueryUrlByOriginalUrlRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(context.Background(), "https://example.com/missing")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchAll_Success() {
	suite.expectKeywords()
	ctx := context.Background()
	first, firstRecord := suite.newMockUrlRow()
	second, secondRecord := suite.newMockUrlRow()

	params := common.NewRequestParams(common.WithRequestLimit(2), common.WithOffset(2))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrls(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUrlsParams) bool {
			return p.LimitBy == 2 && p.CurrentOffset == 2 && !p.UrlStatus.Valid && !p.IncludeDeleted &&
				p.OrderBy == string(common.OrderByCreatedAt) && p.SortOrder == string(common.SortOrderDesc)
		})).
		Return([]postgresql.QueryAllUrlsRow{
			{Url: firstRecord, TotalRecords: 5},
			{Url: secondRecord, TotalRecords: 5},
		}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchAll(ctx, params)
	suite.NoError(err)
	suite.Equal(5, actual.Total)
	suite.Equal(2, actual.Size)
	suite.Equal(2, actual.Page)
	suite.Require().Len(actual.Records, 2)
	suite.Equal(first.ID(), actual.Records[0].ID())
	suite.Equal(second.ID(), actual.Records[1].ID())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchAll_QueryError() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrls(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("boom")).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchAll(context.Background(), common.NewRequestParams())
	suite.Error(err)
	suite.Empty(actual.Records)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByStatus_FiltersByStatus() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow()
	urlRecord.Status = postgresql.UrlStatusEXPIRED

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrls(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUrlsParams) bool {
			return p.UrlStatus.Valid && p.UrlStatus.UrlStatus == postgresql.UrlStatusEXPIRED
		})).
		Return([]postgresql.QueryAllUrlsRow{{Url: urlRecord, TotalRecords: 1}}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByStatus(ctx, domainurl.URLStatusExpired, common.NewRequestParams())
	suite.NoError(err)
	suite.Equal(1, actual.Total)
	suite.Require().Len(actual.Records, 1)
	suite.Equal(mockUrl.ID(), actual.Records[0].ID())
	suite.Equal(domainurl.URLStatusExpired, actual.Records[0].Status())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByStatus_Empty() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrls(gomock.Any(), gomock.Any()).
		Return([]postgresql.QueryAllUrlsRow{}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByStatus(context.Background(), domainurl.URLStatusExpired, common.NewRequestParams())
	suite.NoError(err)
	suite.Equal(0, actual.Total)
	suite.Empty(actual.Records)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByUserId_FiltersByUser() {
	suite.expectKeywords()
	ctx := context.Background()
	userId := entity.NewID()
	mockUrl, urlRecord := suite.newMockUrlRow(urlmock.WithUserId(userId.String()))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrlsByUserId(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUrlsByUserIdParams) bool {
			return p.UserID == pgtype.UUID{Bytes: userId, Valid: true}
		})).
		Return([]postgresql.QueryAllUrlsByUserIdRow{{Url: urlRecord, TotalRecords: 1}}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByUserId(ctx, userId.String(), common.NewRequestParams())
	suite.NoError(err)
	suite.Equal(1, actual.Total)
	suite.Require().Len(actual.Records, 1)
	suite.Equal(mockUrl.ID(), actual.Records[0].ID())
	suite.Equal(userId, actual.Records[0].UserId())
}

// TestFetchByUserId_AppliesTheCallersParams asserts the page window, ordering and status reach the query rather than
// the defaults.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByUserId_AppliesTheCallersParams() {
	suite.expectKeywords()
	userId := entity.NewID()
	_, urlRecord := suite.newMockUrlRow(urlmock.WithUserId(userId.String()))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrlsByUserId(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUrlsByUserIdParams) bool {
			return p.LimitBy == 10 && p.CurrentOffset == 20 &&
				p.OrderBy == string(common.OrderByUpdatedAt) && p.SortOrder == string(common.SortOderAsc) &&
				p.UrlStatus.Valid && p.UrlStatus.UrlStatus == postgresql.UrlStatusEXPIRED
		})).
		Return([]postgresql.QueryAllUrlsByUserIdRow{{Url: urlRecord, TotalRecords: 21}}, nil).
		Times(1)

	params := common.NewRequestParams(
		common.WithRequestLimit(10), common.WithOffset(20),
		common.WithOrderBy(common.OrderByUpdatedAt), common.WithSortOrder(common.SortOderAsc),
		common.WithStatus(string(domainurl.URLStatusExpired)))

	actual, err := suite.urlReadDatastoreAdapter.FetchByUserId(context.Background(), userId.String(), params)
	suite.NoError(err)
	suite.Equal(21, actual.Total)
	suite.Equal(3, actual.Page)
}

// TestFetchByUserId_NeverIncludesDeleted asserts a user's deleted urls stay hidden whatever the params ask for.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByUserId_NeverIncludesDeleted() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrlsByUserId(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUrlsByUserIdParams) bool {
			return !p.IncludeDeleted
		})).
		Return([]postgresql.QueryAllUrlsByUserIdRow{}, nil).
		Times(1)

	_, err := suite.urlReadDatastoreAdapter.FetchByUserId(context.Background(), entity.NewID().String(),
		common.NewRequestParams(common.WithIncludeDeleted(true)))
	suite.NoError(err)
}

// TestFetchByUserId_InvalidId asserts a malformed user id never reaches the database.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByUserId_InvalidId() {
	actual, err := suite.urlReadDatastoreAdapter.FetchByUserId(context.Background(), "not-a-uuid", common.NewRequestParams())
	suite.Error(err)
	suite.Empty(actual.Records)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByUserId_QueryError() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrlsByUserId(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("boom")).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByUserId(context.Background(), entity.NewID().String(), common.NewRequestParams())
	suite.Error(err)
	suite.Empty(actual.Records)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchExpiredActive_ReturnsRows() {
	suite.expectKeywords()
	ctx := context.Background()
	before := time.Now()
	first, firstRecord := suite.newMockUrlRow()
	second, secondRecord := suite.newMockUrlRow()

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryExpiredActiveUrls(gomock.Any(), gomock.Cond(func(p postgresql.QueryExpiredActiveUrlsParams) bool {
			return p.LimitBy == 10 && p.ExpiresBefore.Valid && p.ExpiresBefore.Time.Equal(before)
		})).
		Return([]postgresql.QueryExpiredActiveUrlsRow{
			{Url: firstRecord},
			{Url: secondRecord},
		}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchExpiredActive(ctx, before, 10)
	suite.NoError(err)
	suite.Require().Len(actual, 2)
	suite.Equal(first.ID(), actual[0].ID())
	suite.Equal(second.ID(), actual[1].ID())
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchExpiredActive_Empty() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryExpiredActiveUrls(gomock.Any(), gomock.Any()).
		Return([]postgresql.QueryExpiredActiveUrlsRow{}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchExpiredActive(context.Background(), time.Now(), 10)
	suite.NoError(err)
	suite.Empty(actual)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchExpiredActive_QueryError() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryExpiredActiveUrls(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("boom")).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchExpiredActive(context.Background(), time.Now(), 10)
	suite.Error(err)
	suite.Empty(actual)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestCountExpiredActive_ReturnsTheCount() {
	before := time.Now()
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryCountExpiredActiveUrls(gomock.Any(), gomock.Cond(func(p pgtype.Timestamptz) bool { return p.Valid && p.Time.Equal(before) })).
		Return(int64(7), nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.CountExpiredActive(context.Background(), before)
	suite.NoError(err)
	suite.Equal(int64(7), actual)
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestCountExpiredActive_QueryError() {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryCountExpiredActiveUrls(gomock.Any(), gomock.Any()).
		Return(int64(0), errors.New("boom")).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.CountExpiredActive(context.Background(), time.Now())
	suite.Error(err)
	suite.Zero(actual)
}

// TestFetchByOriginalUrl_NormalizesLookup matters because original_url is stored canonically: a
// caller passing a trailing slash, an uppercase host or a utm parameter must still find the row.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByOriginalUrl_NormalizesLookup() {
	suite.expectKeywords()
	ctx := context.Background()
	mockUrl, urlRecord := suite.newMockUrlRow(urlmock.WithOriginalUrl("https://example.com/target"))

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlByOriginalUrl(gomock.Any(), "https://example.com/target").
		Return(postgresql.QueryUrlByOriginalUrlRow{Url: urlRecord}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(ctx, "HTTPS://EXAMPLE.COM:443/target/?utm_source=news")
	suite.NoError(err)
	suite.Equal(mockUrl.ID(), actual.ID())
}

// TestFetchByOriginalUrl_RejectsMalformedUrl asserts an unstorable URL never reaches the database.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchByOriginalUrl_RejectsMalformedUrl() {
	_, err := suite.urlReadDatastoreAdapter.FetchByOriginalUrl(context.Background(), "nope")
	suite.Error(err)
}

// expectKeywords expects the one keywords lookup every successful read makes, answering it with rows.
func (suite *UrlReadDatastoreAdapterTestSuite) expectKeywords(rows ...postgresql.QueryActiveKeywordsByUrlIdsRow) {
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryActiveKeywordsByUrlIds(gomock.Any(), gomock.Any()).
		Return(rows, nil).
		Times(1)
}

// TestFetchAll_AttachesEachUrlsKeywords asserts the keywords loaded in one query end up on the URL they belong to.
func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchAll_AttachesEachUrlsKeywords() {
	first, firstRecord := suite.newMockUrlRow()
	second, secondRecord := suite.newMockUrlRow()

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryAllUrls(gomock.Any(), gomock.Any()).
		Return([]postgresql.QueryAllUrlsRow{{Url: firstRecord, TotalRecords: 2}, {Url: secondRecord, TotalRecords: 2}}, nil).
		Times(1)
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryActiveKeywordsByUrlIds(gomock.Any(), []pgtype.UUID{firstRecord.ID, secondRecord.ID}).
		Return([]postgresql.QueryActiveKeywordsByUrlIdsRow{
			{Keyword: postgresql.Keyword{UrlID: secondRecord.ID, Value: "news"}},
			{Keyword: postgresql.Keyword{UrlID: firstRecord.ID, Value: "docs"}},
			{Keyword: postgresql.Keyword{UrlID: secondRecord.ID, Value: "tech"}},
		}, nil).
		Times(1)

	actual, err := suite.urlReadDatastoreAdapter.FetchAll(context.Background(), common.NewRequestParams())
	suite.Require().NoError(err)
	suite.Require().Len(actual.Records, 2)

	keywordValues := func(u domainurl.URL) []string {
		values := []string{}
		for _, kw := range u.Keywords() {
			values = append(values, kw.Value)
		}
		return values
	}
	suite.Equal(first.ID(), actual.Records[0].ID())
	suite.Equal([]string{"docs"}, keywordValues(actual.Records[0]))
	suite.Equal(second.ID(), actual.Records[1].ID())
	suite.Equal([]string{"news", "tech"}, keywordValues(actual.Records[1]))
}

func (suite *UrlReadDatastoreAdapterTestSuite) TestFetchById_KeywordsQueryError() {
	mockUrl, urlRecord := suite.newMockUrlRow()

	suite.mockUrlReadQuerier.
		EXPECT().
		QueryUrlById(gomock.Any(), gomock.Any()).
		Return(postgresql.QueryUrlByIdRow{Url: urlRecord}, nil).
		Times(1)
	suite.mockUrlReadQuerier.
		EXPECT().
		QueryActiveKeywordsByUrlIds(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection reset")).
		AnyTimes()

	_, err := suite.urlReadDatastoreAdapter.FetchById(context.Background(), mockUrl.ID().String())
	suite.ErrorContains(err, "connection reset")
}
