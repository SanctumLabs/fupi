package urlrepo

import (
	"fmt"
	"log/slog"
	"time"

	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
)

type UrlMapperParams struct {
	UrlModel postgresql.Url
	Status   string
}

func MapUrlModelToEntity(params UrlMapperParams) (url.URL, error) {
	urlModel := params.UrlModel
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
		ShortCode:     params.UrlModel.ShortCode,
		CustomAlias:   params.UrlModel.CustomAlias.String,
		OriginalUrl:   params.UrlModel.OriginalUrl,
		Status:        url.URLStatus(params.Status),
		ExpiresOn:     params.UrlModel.ExpiresOn.Time,
		OgTitle:       params.UrlModel.OgTitle.String,
		OgDescription: params.UrlModel.OgDescription.String,
		OgImageUrl:    params.UrlModel.OgImageUrl.String,
	}

	urlEntity, urlEntityErr := url.NewUrl(urlParams)
	if urlEntityErr != nil {
		return url.URL{}, fmt.Errorf("failed to map url model to URL entity: %w", urlEntityErr)
	}
	return *urlEntity, nil
}
