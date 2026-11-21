package urldatastore

import (
	"fmt"
	"log/slog"
	"time"

	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

// MapUrlModelToEntity maps a url row and the values of its keywords onto a URL entity. The keywords live in their own
// table, so the caller loads them; a URL mapped without them would lose them on its next Update.
func MapUrlModelToEntity(urlModel postgresql.Url, keywords []string) (url.URL, error) {
	urlId, urlIdErr := postgres.UUIDToString(urlModel.ID)
	if urlIdErr != nil {
		return url.URL{}, fmt.Errorf("failed to parse id when mapping url %v with error %w", urlModel.ID, urlIdErr)
	}
	urlEntityId, urlEntityIdErr := entity.StringToID(urlId)
	if urlEntityIdErr != nil {
		return url.URL{}, fmt.Errorf("failed to parse url entity id when mapping url %v with error %w", urlModel.ID, urlEntityIdErr)
	}

	userId, userIdErr := postgres.UUIDToString(urlModel.UserID)
	if userIdErr != nil {
		return url.URL{}, fmt.Errorf("failed to parse user id when mapping url %v with error %w", urlModel.UserID, userIdErr)
	}

	// Build optional timestamps only when valid
	var deletedAt *time.Time
	if urlModel.DeletedAt.Valid {
		t := urlModel.DeletedAt.Time
		deletedAt = &t
	}

	metadata := map[string]any{}
	if urlModel.Metadata != nil {
		urlMetadata, metadataErr := entity.BytesToMetadata(urlModel.Metadata)
		if metadataErr != nil {
			// Log a warning for failing to parse metadata, instead of failing
			slog.Warn("failed to parse metadata when mapping url, skipping metadata mapping", "metadata", urlModel.Metadata, "error", metadataErr)
		}
		metadata = urlMetadata
	}

	urlParams := url.URLParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID: urlEntityId,
				},
				EntityTimestampParams: entity.EntityTimestampParams{
					CreatedAt: urlModel.CreatedAt.Time,
					UpdatedAt: urlModel.UpdatedAt.Time,
					DeletedAt: deletedAt,
				},
				Metadata: metadata,
			},
		},
		UserId:        userId,
		ShortCode:     urlModel.ShortCode,
		CustomAlias:   urlModel.CustomAlias.String,
		OriginalUrl:   urlModel.OriginalUrl,
		Status:        url.URLStatus(urlModel.Status),
		ExpiresOn:     urlModel.ExpiresOn.Time,
		OgTitle:       urlModel.OgTitle.String,
		OgDescription: urlModel.OgDescription.String,
		OgImageUrl:    urlModel.OgImageUrl.String,
		Keywords:      keywords,
	}

	urlEntity, urlEntityErr := url.NewUrl(urlParams)
	if urlEntityErr != nil {
		return url.URL{}, fmt.Errorf("failed to map url model to URL entity: %w", urlEntityErr)
	}
	return *urlEntity, nil
}
