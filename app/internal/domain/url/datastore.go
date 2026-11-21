package url

import (
	"context"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/ports/repository"
)

type (

	// UrlDatastore defines the interface for interacting with URL entities in an underlying storage implementation
	UrlDatastore interface {
		UrlReadDatastore
		UrlWriteDatastore
	}

	// UrlWriteDatastore defines the interface for writing URL entities to an underlying storage implementation
	UrlWriteDatastore interface {
		repository.WriteRepositoryPort[URL]
	}

	// UrlReadDatastore defines the interface for reading URL entities from an underlying storage implementation
	UrlReadDatastore interface {
		repository.ReadRepositoryPort[URL]

		// FetchByShortCode retrieves a URL entity by its short code
		FetchByShortCode(ctx context.Context, shortCode string) (URL, error)

		// FetchByCustomAlias retrieves a URL entity by its custom alias
		FetchByCustomAlias(ctx context.Context, customAlias string) (URL, error)

		// FetchByUserId retrieves a list of URL entities by the user id of their owner
		FetchByUserId(ctx context.Context, userId string) (repository.FetchRecordsResponse[URL], error)

		// FetchByStatus retrieves a list of URL entities by their status
		FetchByStatus(ctx context.Context, status URLStatus) (repository.FetchRecordsResponse[URL], error)

		// FetchByOriginalUrl retrieves a URL entity by its original url
		FetchByOriginalUrl(ctx context.Context, originalUrl string) (URL, error)

		// FetchExpiredActive retrieves up to limit ACTIVE URL entities whose expiry is before the given time.
		// Used by the expiry background job to find URLs to transition to EXPIRED.
		FetchExpiredActive(ctx context.Context, before time.Time, limit int) ([]URL, error)
	}
)
