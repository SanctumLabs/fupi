package urldatastore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

func NewUrlReadDatastoreAdapter(dbClient database.PostgresDatabaseClient, config database.Config) url.UrlReadDatastore {
	repo := &urlReadDatastoreAdapter{
		dbClient:  dbClient,
		logPrefix: "UrlReadDatastoreAdapter",
		config:    config,
	}

	// Wire up the real transaction executor. This delegates to postgres.WithTransactionVoid,
	// which handles the pgxpool.Pool lifecycle. Tests override this field directly.
	repo.withTx = func(ctx context.Context, fn func(q postgresrepo.UrlReadQuerier) error) error {
		return postgres.WithTransactionVoid(ctx, dbClient, func(qtx *postgresql.Queries) error {
			// *postgresql.Queries satisfies UrlReadQuerier, so we can pass it straight through.
			return fn(qtx)
		})
	}

	return repo
}

// fetchOne runs a single-row lookup and maps the result, translating pgx.ErrNoRows to a NotFound error.
func (repo *urlReadDatastoreAdapter) fetchOne(
	ctx context.Context,
	operation string,
	logAttrs []any,
	query func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (postgresql.Url, error),
) (url.URL, error) {
	handlerLogPrefix := fmt.Sprintf("%s<%s>", repo.logPrefix, operation)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.%s", repo.logPrefix, operation), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (url.URL, error) {
			urlModel, queryErr := query(ctx, qtx)
			if queryErr != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to retrieve URL", handlerLogPrefix), append(logAttrs, "error", queryErr)...)
				if errors.Is(queryErr, pgx.ErrNoRows) {
					return url.URL{}, errdefs.NotFound(queryErr)
				}
				return url.URL{}, errdefs.BadRequest(queryErr)
			}

			urls, mapErr := mapUrls(ctx, qtx, []postgresql.Url{urlModel})
			if mapErr != nil {
				return url.URL{}, mapErr
			}
			return urls[0], nil
		})
}

// mapUrls maps url rows onto entities, loading the keywords of all of them in one query.
func mapUrls(ctx context.Context, qtx postgresrepo.UrlReadQuerier, urlModels []postgresql.Url) ([]url.URL, error) {
	if len(urlModels) == 0 {
		return []url.URL{}, nil
	}

	ids := make([]pgtype.UUID, 0, len(urlModels))
	for _, urlModel := range urlModels {
		ids = append(ids, urlModel.ID)
	}

	keywordRows, keywordsErr := qtx.QueryActiveKeywordsByUrlIds(ctx, ids)
	if keywordsErr != nil {
		return nil, fmt.Errorf("failed to load url keywords: %w", keywordsErr)
	}
	keywordsByUrl := make(map[[16]byte][]string, len(urlModels))
	for _, row := range keywordRows {
		keywordsByUrl[row.Keyword.UrlID.Bytes] = append(keywordsByUrl[row.Keyword.UrlID.Bytes], row.Keyword.Value)
	}

	urls := make([]url.URL, 0, len(urlModels))
	for _, urlModel := range urlModels {
		urlEntity, mapErr := MapUrlModelToEntity(urlModel, keywordsByUrl[urlModel.ID.Bytes])
		if mapErr != nil {
			return nil, mapErr
		}
		urls = append(urls, urlEntity)
	}
	return urls, nil
}

func (repo *urlReadDatastoreAdapter) FetchById(ctx context.Context, id string) (url.URL, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchById> Fetching URL by ID", repo.logPrefix), "id", id)

	return repo.fetchOne(ctx, "FetchById", []any{"id", id},
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (postgresql.Url, error) {
			urlUUID, urlUUIDErr := postgres.StringToUUID(id)
			if urlUUIDErr != nil {
				return postgresql.Url{}, fmt.Errorf("failed to convert url ID to UUID: %w", urlUUIDErr)
			}
			row, err := qtx.QueryUrlById(ctx, urlUUID)
			return row.Url, err
		})
}

func (repo *urlReadDatastoreAdapter) FetchByShortCode(ctx context.Context, shortCode string) (url.URL, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByShortCode> Fetching URL by short code", repo.logPrefix), "short_code", shortCode)

	return repo.fetchOne(ctx, "FetchByShortCode", []any{"short_code", shortCode},
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (postgresql.Url, error) {
			row, err := qtx.QueryUrlByUrlShortCode(ctx, shortCode)
			return row.Url, err
		})
}

// FetchRedirectTarget is the redirect's read: one query for the short code or the alias, and no keywords.
func (repo *urlReadDatastoreAdapter) FetchRedirectTarget(ctx context.Context, code string) (url.RedirectTarget, error) {
	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.FetchRedirectTarget", repo.logPrefix), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (url.RedirectTarget, error) {
			row, queryErr := qtx.QueryRedirectTargetByCode(ctx, code)
			if errors.Is(queryErr, pgx.ErrNoRows) {
				return url.RedirectTarget{}, errdefs.NotFound(queryErr)
			}
			if queryErr != nil {
				return url.RedirectTarget{}, fmt.Errorf("failed to retrieve redirect target: %w", queryErr)
			}
			return url.NewRedirectTarget(row.OriginalUrl, url.URLStatus(row.Status), row.ExpiresOn.Time)
		})
}

func (repo *urlReadDatastoreAdapter) FetchByCustomAlias(ctx context.Context, customAlias string) (url.URL, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByCustomAlias> Fetching URL by custom alias", repo.logPrefix), "custom_alias", customAlias)

	return repo.fetchOne(ctx, "FetchByCustomAlias", []any{"custom_alias", customAlias},
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (postgresql.Url, error) {
			row, err := qtx.QueryUrlByCustomAlias(ctx, pgtype.Text{String: customAlias, Valid: customAlias != ""})
			return row.Url, err
		})
}

// FetchByOriginalUrl returns the URL for the given target, of which there is at most one:
// idx_urls_original_url makes original_url unique among non-deleted rows.
//
// The argument is put through the OriginalURL value object first, so a caller passing a non-canonical
// spelling still matches the canonical form that was stored. A URL that cannot be stored at all is
// rejected without going to the database.
func (repo *urlReadDatastoreAdapter) FetchByOriginalUrl(ctx context.Context, originalUrl string) (url.URL, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByOriginalUrl> Fetching URL by original url", repo.logPrefix), "original_url", originalUrl)

	canonical, canonicalErr := url.NewOriginalURL(originalUrl)
	if canonicalErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s<FetchByOriginalUrl> Rejecting malformed original url", repo.logPrefix),
			"original_url", originalUrl, "error", canonicalErr)
		return url.URL{}, errdefs.InvalidParameter(canonicalErr)
	}

	return repo.fetchOne(ctx, "FetchByOriginalUrl", []any{"original_url", canonical.Value()},
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (postgresql.Url, error) {
			row, err := qtx.QueryUrlByOriginalUrl(ctx, canonical.Value())
			return row.Url, err
		})
}

func (repo *urlReadDatastoreAdapter) FetchAll(ctx context.Context, params common.RequestParams) (repository.FetchRecordsResponse[url.URL], error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchAll> Fetching URLs", repo.logPrefix), "params", params)

	queryParams := toQueryAllUrlsParams(params, nil)

	return repo.fetchMany(ctx, "FetchAll", int(queryParams.CurrentOffset), int(queryParams.LimitBy),
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) ([]postgresql.Url, int64, error) {
			rows, queryErr := qtx.QueryAllUrls(ctx, queryParams)
			if queryErr != nil {
				return nil, 0, queryErr
			}

			urlModels := make([]postgresql.Url, 0, len(rows))
			var total int64
			for _, row := range rows {
				total = row.TotalRecords
				urlModels = append(urlModels, row.Url)
			}
			return urlModels, total, nil
		})
}

func (repo *urlReadDatastoreAdapter) FetchByStatus(ctx context.Context, status url.URLStatus, params common.RequestParams) (repository.FetchRecordsResponse[url.URL], error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByStatus> Fetching URLs by status", repo.logPrefix), "status", status)

	queryParams := toQueryAllUrlsParams(params, &status)

	return repo.fetchMany(ctx, "FetchByStatus", int(queryParams.CurrentOffset), int(queryParams.LimitBy),
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) ([]postgresql.Url, int64, error) {
			rows, queryErr := qtx.QueryAllUrls(ctx, queryParams)
			if queryErr != nil {
				return nil, 0, queryErr
			}

			urlModels := make([]postgresql.Url, 0, len(rows))
			var total int64
			for _, row := range rows {
				total = row.TotalRecords
				urlModels = append(urlModels, row.Url)
			}
			return urlModels, total, nil
		})
}

func (repo *urlReadDatastoreAdapter) FetchByUserId(ctx context.Context, userId string, params common.RequestParams) (repository.FetchRecordsResponse[url.URL], error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByUserId> Fetching URLs by user id", repo.logPrefix), "user_id", userId)

	// A user never sees their deleted urls, so the caller's IncludeDeleted is not honoured here.
	params.IncludeDeleted = false

	var status *url.URLStatus
	if params.Status != "" {
		parsed := url.URLStatus(params.Status)
		status = &parsed
	}
	allUrlsParams := toQueryAllUrlsParams(params, status)

	return repo.fetchMany(ctx, "FetchByUserId", int(allUrlsParams.CurrentOffset), int(allUrlsParams.LimitBy),
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) ([]postgresql.Url, int64, error) {
			userUUID, userUUIDErr := postgres.StringToUUID(userId)
			if userUUIDErr != nil {
				return nil, 0, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
			}

			rows, queryErr := qtx.QueryAllUrlsByUserId(ctx, postgresql.QueryAllUrlsByUserIdParams{
				UserID:         userUUID,
				IncludeDeleted: allUrlsParams.IncludeDeleted,
				UrlStatus:      allUrlsParams.UrlStatus,
				DateField:      allUrlsParams.DateField,
				DateFrom:       allUrlsParams.DateFrom,
				DateTo:         allUrlsParams.DateTo,
				OrderBy:        allUrlsParams.OrderBy,
				SortOrder:      allUrlsParams.SortOrder,
				CurrentOffset:  allUrlsParams.CurrentOffset,
				LimitBy:        allUrlsParams.LimitBy,
			})
			if queryErr != nil {
				return nil, 0, queryErr
			}

			urlModels := make([]postgresql.Url, 0, len(rows))
			var total int64
			for _, row := range rows {
				total = row.TotalRecords
				urlModels = append(urlModels, row.Url)
			}
			return urlModels, total, nil
		})
}

// FetchExpiredActive returns ACTIVE URLs whose expiry has already passed. It is unpaginated: the
// expiry job drains the backlog in limit-sized batches, so a total count would be noise.
func (repo *urlReadDatastoreAdapter) FetchExpiredActive(ctx context.Context, before time.Time, limit int) ([]url.URL, error) {
	operation := "FetchExpiredActive"
	handlerLogPrefix := fmt.Sprintf("%s<%s>", repo.logPrefix, operation)
	slog.InfoContext(ctx, fmt.Sprintf("%s Fetching expired active URLs", handlerLogPrefix), "before", before, "limit", limit)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.%s", repo.logPrefix, operation), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) ([]url.URL, error) {
			rows, queryErr := qtx.QueryExpiredActiveUrls(ctx, postgresql.QueryExpiredActiveUrlsParams{
				ExpiresBefore: pgtype.Timestamptz{Time: before, Valid: !before.IsZero()},
				LimitBy:       int32(limit),
			})
			if queryErr != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to retrieve expired active URLs", handlerLogPrefix),
					"before", before, "limit", limit, "error", queryErr)
				return nil, errdefs.BadRequest(queryErr)
			}

			urlModels := make([]postgresql.Url, 0, len(rows))
			for _, row := range rows {
				urlModels = append(urlModels, row.Url)
			}

			return mapUrls(ctx, qtx, urlModels)
		})
}

// fetchMany runs a paginated URLs query and maps every row, returning the page and the total count.
// query reports the rows alongside the window's total so each caller can supply its own SQL.
func (repo *urlReadDatastoreAdapter) fetchMany(
	ctx context.Context,
	operation string,
	offset int,
	limit int,
	query func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) ([]postgresql.Url, int64, error),
) (repository.FetchRecordsResponse[url.URL], error) {
	handlerLogPrefix := fmt.Sprintf("%s<%s>", repo.logPrefix, operation)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.%s", repo.logPrefix, operation), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UrlReadQuerier) (repository.FetchRecordsResponse[url.URL], error) {
			urlModels, total, queryErr := query(ctx, qtx)
			if queryErr != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to retrieve URLs", handlerLogPrefix), "error", queryErr)
				return repository.FetchRecordsResponse[url.URL]{}, errdefs.BadRequest(queryErr)
			}

			urls, mapErr := mapUrls(ctx, qtx, urlModels)
			if mapErr != nil {
				return repository.FetchRecordsResponse[url.URL]{}, mapErr
			}

			page := 1
			if limit > 0 {
				page = offset/limit + 1
			}

			return repository.FetchRecordsResponse[url.URL]{
				Records: urls,
				Total:   int(total),
				Page:    page,
				Size:    len(urls),
			}, nil
		})
}

// toQueryAllUrlsParams maps generic request params onto the sqlc-generated query params.
// A nil status means no status filter.
func toQueryAllUrlsParams(params common.RequestParams, status *url.URLStatus) postgresql.QueryAllUrlsParams {
	queryParams := postgresql.QueryAllUrlsParams{
		IncludeDeleted: params.IncludeDeleted,
		OrderBy:        string(params.OrderOption.OrderBy),
		SortOrder:      string(params.OrderOption.SortOrder),
		CurrentOffset:  int32(params.Offset),
		LimitBy:        int32(params.Limit),
	}
	if status != nil {
		queryParams.UrlStatus = postgresql.NullUrlStatus{UrlStatus: postgresql.UrlStatus(*status), Valid: true}
	}
	if params.DateRangeOption.DateFieldOption != "" {
		queryParams.DateField = pgtype.Text{String: string(params.DateRangeOption.DateFieldOption), Valid: true}
		queryParams.DateFrom = pgtype.Timestamp{Time: params.DateRangeOption.StartDate, Valid: !params.DateRangeOption.StartDate.IsZero()}
		queryParams.DateTo = pgtype.Timestamp{Time: params.DateRangeOption.EndDate, Valid: !params.DateRangeOption.EndDate.IsZero()}
	}
	return queryParams
}
