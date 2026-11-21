//go:build legacy

package contracts

import (
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entities"
)

// Url
type UrlService interface {
	LookupUrl(shortCode string) (string, error)
}

// UrlReadService performs read operations to get URLs
type UrlReadService interface {
	GetByShortCode(shortCode string) (entities.URL, error)
	GetByUserId(userID string) ([]entities.URL, error)
	GetByKeyword(keyword string) ([]entities.URL, error)
	GetByKeywords(keywords []string) ([]entities.URL, error)
	GetByOriginalUrl(originalUrl string) (entities.URL, error)
	GetById(id string) (entities.URL, error)
}

// UrlWriteService performs write operations on URLs
type UrlWriteService interface {
	CreateUrl(userID string, originalUrl string, customAlias string, expiresOn time.Time, keywords []string) (entities.URL, error)
	UpdateUrl(url UpdateUrlRequest) (entities.URL, error)
	Remove(id string) error
}
