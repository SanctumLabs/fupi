package urldatastore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
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
			if conflictErr := asConflict(createdUrlErr); conflictErr != nil {
				return url.URL{}, conflictErr
			}
			return url.URL{}, fmt.Errorf("failed to create URL: %w", createdUrlErr)
		}

		keywords, keywordsErr := createKeywords(retryCtx, handlerLogPrefix, qtx, createdUrl.ID, urlEntity.Keywords())
		if keywordsErr != nil {
			return url.URL{}, keywordsErr
		}

		if outboxErr := writeEvents(retryCtx, qtx, urlEntity); outboxErr != nil {
			return url.URL{}, outboxErr
		}

		return mapUrl(retryCtx, handlerLogPrefix, createdUrl, keywords)
	})
}

// Update persists the state the URL aggregate owns after a change: its status, expiry and keywords, with the events
// it recorded, in one transaction. The keywords are replaced wholesale, so the entity must carry all of them.
func (repo *urlWriteDatastoreAdapter) Update(ctx context.Context, urlEntity url.URL) (url.URL, error) {
	handlerLogPrefix := fmt.Sprintf("%s<Update>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Updating URL", handlerLogPrefix), "url_id", urlEntity.ID(), "status", urlEntity.Status())

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.Update", repo.logPrefix), repo.withTx, func(retryCtx context.Context, qtx postgresrepo.UrlWriteQuerier) (url.URL, error) {
		urlID := pgtype.UUID{Bytes: urlEntity.ID(), Valid: true}

		updatedUrl, updateErr := qtx.QueryUpdateUrl(retryCtx, postgresql.QueryUpdateUrlParams{
			ID:        urlID,
			Status:    postgresql.UrlStatus(urlEntity.Status()),
			ExpiresOn: pgtype.Timestamptz{Time: urlEntity.ExpiresOn(), Valid: !urlEntity.ExpiresOn().IsZero()},
		})
		if updateErr != nil {
			slog.ErrorContext(retryCtx, fmt.Sprintf("%s Failed to update URL", handlerLogPrefix), "url_id", urlEntity.ID(), "error", updateErr)
			if errors.Is(updateErr, pgx.ErrNoRows) {
				return url.URL{}, errdefs.NotFound(updateErr)
			}
			return url.URL{}, fmt.Errorf("failed to update URL %s: %w", urlEntity.ID(), updateErr)
		}

		if deleteErr := qtx.QueryDeleteKeywordsByUrlId(retryCtx, urlID); deleteErr != nil {
			slog.ErrorContext(retryCtx, fmt.Sprintf("%s Failed to clear URL keywords", handlerLogPrefix), "url_id", urlEntity.ID(), "error", deleteErr)
			return url.URL{}, fmt.Errorf("failed to clear keywords of URL %s: %w", urlEntity.ID(), deleteErr)
		}

		keywords, keywordsErr := createKeywords(retryCtx, handlerLogPrefix, qtx, urlID, urlEntity.Keywords())
		if keywordsErr != nil {
			return url.URL{}, keywordsErr
		}

		if outboxErr := writeEvents(retryCtx, qtx, urlEntity); outboxErr != nil {
			return url.URL{}, outboxErr
		}

		return mapUrl(retryCtx, handlerLogPrefix, updatedUrl, keywords)
	})
}

// urlEventsDestination is where the URL context's domain events are published once relayed from the outbox.
const urlEventsDestination = "url.events"

// writeEvents appends the events the url aggregate recorded to the outbox, in the caller's transaction. The URL
// returned by a write is re-mapped from the database and carries none, so events are never written twice.
func writeEvents(ctx context.Context, qtx postgresrepo.UrlWriteQuerier, urlEntity url.URL) error {
	return postgresrepo.WriteOutboxEvents(ctx, qtx, urlEventsDestination, entity.IDToString(urlEntity.ID()), urlEntity.DomainEvents())
}

// createKeywords inserts the keywords of a URL and returns their values. A failed statement aborts the transaction,
// and ADR-0003 forbids silently dropping a keyword, so the first failure fails the whole write.
func createKeywords(ctx context.Context, handlerLogPrefix string, qtx postgresrepo.UrlWriteQuerier, urlID pgtype.UUID, keywords []url.Keyword) ([]string, error) {
	values := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if _, createKeywordErr := qtx.QueryCreateKeyword(ctx, postgresql.QueryCreateKeywordParams{
			UrlID: urlID,
			Value: keyword.Value,
		}); createKeywordErr != nil {
			slog.ErrorContext(
				ctx,
				fmt.Sprintf("%s Failed to create keyword for URL", handlerLogPrefix),
				"url_id", urlID,
				"keyword", keyword.Value,
				"error", createKeywordErr,
			)
			if conflictErr := asConflict(createKeywordErr); conflictErr != nil {
				return nil, conflictErr
			}
			return nil, fmt.Errorf("failed to create keyword %q for URL: %w", keyword.Value, createKeywordErr)
		}
		values = append(values, keyword.Value)
	}
	return values, nil
}

// mapUrl maps a written url row back onto an entity, logging and wrapping any mapping failure.
func mapUrl(ctx context.Context, handlerLogPrefix string, urlModel postgresql.Url, keywords []string) (url.URL, error) {
	mappedUrl, mapErr := MapUrlModelToEntity(urlModel, keywords)
	if mapErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to map URL model to entity", handlerLogPrefix), "url_id", urlModel.ID, "error", mapErr)
		return url.URL{}, fmt.Errorf("failed to map URL model to entity: %w", mapErr)
	}
	return mappedUrl, nil
}

// SoftDelete marks the url DELETED through the aggregate, so the status, deleted_at (which releases the target for
// re-shortening) and URLDeleted commit together. The row is locked while it is decided, so of two concurrent deletes
// one succeeds and the other is a Conflict. An unknown id is NotFound.
func (repo *urlWriteDatastoreAdapter) SoftDelete(ctx context.Context, id string) error {
	handlerLogPrefix := fmt.Sprintf("%s<SoftDelete>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Soft deleting URL", handlerLogPrefix), "url_id", id)

	_, err := postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.SoftDelete", repo.logPrefix), repo.withTx, func(retryCtx context.Context, qtx postgresrepo.UrlWriteQuerier) (url.URL, error) {
		locked, urlID, lockErr := lockUrl(retryCtx, handlerLogPrefix, qtx, id)
		if lockErr != nil {
			return url.URL{}, lockErr
		}

		if deleteErr := locked.MarkDeleted(); deleteErr != nil {
			return url.URL{}, errdefs.Conflict(deleteErr)
		}

		if _, updateErr := qtx.QueryUpdateUrl(retryCtx, postgresql.QueryUpdateUrlParams{
			ID:        urlID,
			Status:    postgresql.UrlStatus(locked.Status()),
			ExpiresOn: pgtype.Timestamptz{Time: locked.ExpiresOn(), Valid: !locked.ExpiresOn().IsZero()},
		}); updateErr != nil {
			slog.ErrorContext(retryCtx, fmt.Sprintf("%s Failed to soft delete URL", handlerLogPrefix), "url_id", id, "error", updateErr)
			return url.URL{}, fmt.Errorf("failed to soft delete URL %s: %w", id, updateErr)
		}

		return url.URL{}, writeEvents(retryCtx, qtx, locked)
	})
	return err
}

// Delete removes the url for good, with its keywords and scan results (ON DELETE CASCADE), and records URLPurged in the
// same transaction so consumers drop what they hold for it. It works whatever the url's status. An unknown id is
// NotFound.
func (repo *urlWriteDatastoreAdapter) Delete(ctx context.Context, id string) error {
	handlerLogPrefix := fmt.Sprintf("%s<Delete>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Purging URL", handlerLogPrefix), "url_id", id)

	_, err := postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.Delete", repo.logPrefix), repo.withTx, func(retryCtx context.Context, qtx postgresrepo.UrlWriteQuerier) (url.URL, error) {
		locked, urlID, lockErr := lockUrl(retryCtx, handlerLogPrefix, qtx, id)
		if lockErr != nil {
			return url.URL{}, lockErr
		}

		locked.Purge()

		if _, deleteErr := qtx.QueryDeleteUrlWithId(retryCtx, urlID); deleteErr != nil {
			slog.ErrorContext(retryCtx, fmt.Sprintf("%s Failed to purge URL", handlerLogPrefix), "url_id", id, "error", deleteErr)
			return url.URL{}, fmt.Errorf("failed to purge URL %s: %w", id, deleteErr)
		}

		return url.URL{}, writeEvents(retryCtx, qtx, locked)
	})
	return err
}

// lockUrl loads the url with the given id and locks its row for the rest of the transaction. The aggregate is mapped
// without its keywords: the callers decide its status and must not write keywords back.
func lockUrl(ctx context.Context, handlerLogPrefix string, qtx postgresrepo.UrlWriteQuerier, id string) (url.URL, pgtype.UUID, error) {
	urlID, idErr := postgres.StringToUUID(id)
	if idErr != nil {
		return url.URL{}, pgtype.UUID{}, errdefs.NotFound(fmt.Errorf("url %q: %w", id, idErr))
	}

	urlModel, queryErr := qtx.QueryUrlByIdForUpdate(ctx, urlID)
	if queryErr != nil {
		if errors.Is(queryErr, pgx.ErrNoRows) {
			return url.URL{}, pgtype.UUID{}, errdefs.NotFound(fmt.Errorf("url %s: %w", id, queryErr))
		}
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to lock URL", handlerLogPrefix), "url_id", id, "error", queryErr)
		return url.URL{}, pgtype.UUID{}, fmt.Errorf("failed to lock URL %s: %w", id, queryErr)
	}

	locked, mapErr := mapUrl(ctx, handlerLogPrefix, urlModel, nil)
	if mapErr != nil {
		return url.URL{}, pgtype.UUID{}, mapErr
	}
	return locked, urlID, nil
}
