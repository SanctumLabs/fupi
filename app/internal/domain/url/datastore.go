package url

import (
	"context"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
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

		// FetchRedirectTarget retrieves what the redirect needs for a short code or, failing that, a custom alias,
		// without the url's keywords. A short code wins over an alias of the same spelling.
		FetchRedirectTarget(ctx context.Context, code string) (RedirectTarget, error)

		// FetchByCustomAlias retrieves a URL entity by its custom alias
		FetchByCustomAlias(ctx context.Context, customAlias string) (URL, error)

		// FetchByUserId retrieves a page of the URL entities owned by the user. Deleted URLs are never returned,
		// whatever params.IncludeDeleted says; params.Status optionally narrows the page to one status.
		FetchByUserId(ctx context.Context, userId string, params common.RequestParams) (repository.FetchRecordsResponse[URL], error)

		// FetchByStatus retrieves a page of the URL entities in the given status
		FetchByStatus(ctx context.Context, status URLStatus, params common.RequestParams) (repository.FetchRecordsResponse[URL], error)

		// FetchByOriginalUrl retrieves a URL entity by its original url
		FetchByOriginalUrl(ctx context.Context, originalUrl string) (URL, error)

		// FetchExpiredActive retrieves up to limit ACTIVE URL entities whose expiry is before the given time.
		// Used by the expiry background job to find URLs to transition to EXPIRED.
		FetchExpiredActive(ctx context.Context, before time.Time, limit int) ([]URL, error)
	}
)
