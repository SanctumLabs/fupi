package urlrepo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
)

func NewUrlWriteRepoAdapter(dbClient database.PostgresDatabaseClient, userRepo identity.UserReadRepository, config database.Config) url.UrlWriteRepository {
	repo := &urlWriteRepositoryAdapter{
		dbClient:  dbClient,
		config:    config,
		logPrefix: "UrlWriteRepoAdapter",
		userRepo:  userRepo,
	}

	// Wire up the real transaction executor. This delegates to postgres.WithTransaction,
	// which handles the pgxpool.Pool lifecycle. Tests override this field directly.
	repo.withTx = func(ctx context.Context, fn func(q postgresrepo.UrlWriteQuerier) (url.URL, error)) (url.URL, error) {
		return postgres.WithTransaction(ctx, dbClient, func(qtx *postgresql.Queries) (url.URL, error) {
			// *postgresql.Queries satisfies urlWriteQuerier, so we can pass it straight through.
			return fn(qtx)
		})
	}

	return repo
}

func (repo *urlWriteRepositoryAdapter) Create(ctx context.Context, urlEntity url.URL) (url.URL, error) {
	handlerLogPrefix := fmt.Sprintf("%s<Create>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Creating URL", handlerLogPrefix), "url", urlEntity)

	operationCtx, operationCancel := context.WithTimeout(ctx, repo.config.OperationTimeout)
	defer operationCancel()

	return recoveryutils.ExecuteWithRetry(
		operationCtx,
		func(retryCtx context.Context) (url.URL, error) {
			// Use repo.withTx instead of postgres.WithTransaction directly.
			// This is the only change to the business logic — everything inside
			// the closure is identical to the original implementation.
			return repo.withTx(retryCtx, func(qtx postgresrepo.UrlWriteQuerier) (url.URL, error) {
				// Check context before proceeding
				select {
				case <-retryCtx.Done():
					slog.ErrorContext(retryCtx, "Operation cancelled before validation with error", "error", retryCtx.Err())
					return url.URL{}, fmt.Errorf("operation cancelled before validation: %w", retryCtx.Err())
				default:
				}

				userId := urlEntity.UserId()

				// check if the user exists before proceeding
				_, userErr := repo.userRepo.FetchById(ctx, userId.String())
				if userErr != nil {
					slog.ErrorContext(ctx, "Failed to fetch user", "user_id", userId.String(), "error", userErr)
					return url.URL{}, fmt.Errorf("failed to fetch user: %w", userErr)
				}

				// Now that we know the user exists, we can proceed with creating the URL
				userUUID, userUUIDErr := postgres.StringToUUID(userId.String())
				if userUUIDErr != nil {
					return url.URL{}, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
				}

				// query the status ID
				status, statusErr := qtx.QueryUrlStatusByName(retryCtx, string(urlEntity.Status()))
				if statusErr != nil {
					slog.ErrorContext(
						retryCtx,
						fmt.Sprintf("%s Failed to retrieve URL status", handlerLogPrefix),
						"url_status", urlEntity.Status(),
						"error", statusErr,
					)
					if errors.Is(statusErr, pgx.ErrNoRows) {
						return url.URL{}, errdefs.NotFound(statusErr)
					}

					return url.URL{}, fmt.Errorf("failed to query URL status: %w", statusErr)
				}

				shortCode := urlEntity.ShortCode()
				customAlias := urlEntity.CustomAlias()
				originalUrl := urlEntity.OriginalURL()

				metadata, metadataErr := urlEntity.MetadataToBytes()
				if metadataErr != nil {
					slog.WarnContext(ctx, fmt.Sprintf("%s Failed to convert bid metadata to bytes", handlerLogPrefix),
						"url", urlEntity,
						"error", metadataErr)
				}

				createdUrl, createdUrlErr := qtx.QueryCreateUrl(
					retryCtx,
					postgresql.QueryCreateUrlParams{
						UserID:    userUUID,
						ShortCode: shortCode.Value(),
						CustomAlias: pgtype.Text{
							String: customAlias.Value(),
							Valid:  customAlias.Value() != "",
						},
						OriginalUrl: originalUrl.Value(),
						StatusID:    status.ID,
						ExpiresOn: pgtype.Timestamptz{
							Time:  urlEntity.ExpiresOn(),
							Valid: !urlEntity.ExpiresOn().IsZero(),
						},
						OgTitle: pgtype.Text{
							String: urlEntity.OgTitle(),
							Valid:  urlEntity.OgTitle() != "",
						},
						OgDescription: pgtype.Text{
							String: urlEntity.OgDescription(),
							Valid:  urlEntity.OgDescription() != "",
						},
						OgImageUrl: pgtype.Text{
							String: urlEntity.OgImageUrl(),
							Valid:  urlEntity.OgImageUrl() != "",
						},
						Metadata: metadata,
					},
				)
				if createdUrlErr != nil {
					slog.ErrorContext(
						retryCtx,
						fmt.Sprintf("%s Failed to create URL", handlerLogPrefix),
						"user_id", userId,
						"short_code", shortCode.Value(),
						"original_url", originalUrl.Value(),
						"error", createdUrlErr,
					)
					return url.URL{}, fmt.Errorf("failed to create URL: %w", createdUrlErr)
				}

				// Insert associated keywords for the created URL
				for _, keyword := range urlEntity.Keywords() {
					_, createKeywordErr := qtx.QueryCreateKeyword(retryCtx, postgresql.QueryCreateKeywordParams{
						UrlID: createdUrl.ID,
						Value: keyword.Value,
					})
					if createKeywordErr != nil {
						slog.ErrorContext(
							retryCtx,
							fmt.Sprintf("%s Failed to create keyword for URL", handlerLogPrefix),
							"url_id", createdUrl.ID,
							"keyword", keyword.Value,
							"error", createKeywordErr,
						)
						// We log a warning, but wwe don't fail the entire operation if keyword creation fails, since the URL itself was created successfully.
						// This is a design choice that can be revisited based on requirements.
					}
				}

				// Map the created URL model back to an entity to return
				mappedUrl, mapErr := MapUrlModelToEntity(UrlMapperParams{
					UrlModel: createdUrl,
					Status:   status.Name,
				})
				if mapErr != nil {
					slog.ErrorContext(
						retryCtx,
						fmt.Sprintf("%s Failed to map created URL model to entity", handlerLogPrefix),
						"url_model", createdUrl,
						"error", mapErr,
					)
					return url.URL{}, fmt.Errorf("failed to map created URL model to entity: %w", mapErr)
				}
				slog.InfoContext(retryCtx, "created model", "url", mappedUrl)

				return mappedUrl, nil
			})
		},
		repo.config.RetryConfig,
		fmt.Sprintf("%s.Create", repo.logPrefix),
	)
}

func (repo *urlWriteRepositoryAdapter) Update(ctx context.Context, urlEntity url.URL) (url.URL, error) {
	return urlEntity, nil
}

func (repo *urlWriteRepositoryAdapter) SoftDelete(ctx context.Context, id string) error {
	panic("not implemented")
}

// Delete deletes a given entity by its ID
func (repo *urlWriteRepositoryAdapter) Delete(ctx context.Context, id string) error {
	panic("not implemented")
}
