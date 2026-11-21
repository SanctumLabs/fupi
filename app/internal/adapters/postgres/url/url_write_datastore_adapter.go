package urldatastore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
)

func NewUrlWriteDatastoreAdapter(dbClient database.PostgresDatabaseClient, config database.Config) url.UrlWriteDatastore {
	repo := &urlWriteDatastoreAdapter{
		dbClient:  dbClient,
		config:    config,
		logPrefix: "UrlWriteDatastoreAdapter",
	}

	// Wire up the real transaction executor. This delegates to postgres.WithTransactionVoid,
	// which handles the pgxpool.Pool lifecycle. Tests override this field directly.
	repo.withTx = func(ctx context.Context, fn func(q postgresrepo.UrlWriteQuerier) error) error {
		return postgres.WithTransactionVoid(ctx, dbClient, func(qtx *postgresql.Queries) error {
			// *postgresql.Queries satisfies UrlWriteQuerier, so we can pass it straight through.
			return fn(qtx)
		})
	}

	return repo
}

func (repo *urlWriteDatastoreAdapter) Save(ctx context.Context, urlEntity url.URL) (url.URL, error) {
	handlerLogPrefix := fmt.Sprintf("%s<Save>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Saving URL", handlerLogPrefix), "url", urlEntity)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.Save", repo.logPrefix), repo.withTx, func(retryCtx context.Context, qtx postgresrepo.UrlWriteQuerier) (url.URL, error) {
		userId := urlEntity.UserId()

		userUUID, userUUIDErr := postgres.StringToUUID(userId.String())
		if userUUIDErr != nil {
			return url.URL{}, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
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
				ID:        pgtype.UUID{Bytes: urlEntity.ID(), Valid: true},
				UserID:    userUUID,
				ShortCode: shortCode.Value(),
				CustomAlias: pgtype.Text{
					String: customAlias.Value(),
					Valid:  customAlias.Value() != "",
				},
				OriginalUrl: originalUrl.Value(),
				Status:      postgresql.UrlStatus(urlEntity.Status()),
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
				// A failed statement aborts the transaction, and ADR-0003 forbids silently dropping a keyword.
				return url.URL{}, fmt.Errorf("failed to create keyword %q for URL: %w", keyword.Value, createKeywordErr)
			}
		}

		// Map the created URL model back to an entity to return
		mappedUrl, mapErr := MapUrlModelToEntity(createdUrl)
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
}

// Update is not implemented yet. It fails loudly rather than reporting a write that never happened.
func (repo *urlWriteDatastoreAdapter) Update(ctx context.Context, urlEntity url.URL) (url.URL, error) {
	return url.URL{}, errdefs.NotImplemented(errors.New("url update is not implemented"))
}

// SoftDelete is not implemented yet.
func (repo *urlWriteDatastoreAdapter) SoftDelete(ctx context.Context, id string) error {
	return errdefs.NotImplemented(errors.New("url soft delete is not implemented"))
}

// Delete is not implemented yet.
func (repo *urlWriteDatastoreAdapter) Delete(ctx context.Context, id string) error {
	return errdefs.NotImplemented(errors.New("url delete is not implemented"))
}
