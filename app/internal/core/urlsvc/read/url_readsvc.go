//go:build legacy

package read

import (
	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
)

//UrlReadSvc represents a url service use case
type UrlReadSvc struct {
	// repo is an interface used to perform CRUD operations on URL records
	repo contracts.UrlReadRepository
	// userSvc is an interface used to interact with the user service use cases
	userSvc contracts.UserService
	// cache is an interface used to interact with cache service
	cache contracts.CacheService
}

// NewUrlSvc creates a new url service
func NewUrlReadSvc(urlRepository contracts.UrlReadRepository, userSvc contracts.UserService, cacheSvc contracts.CacheService) *UrlReadSvc {
	return &UrlReadSvc{urlRepository, userSvc, cacheSvc}
}

// GetByShortCode returns shortened url given its short code
func (svc *UrlReadSvc) GetByShortCode(shortCode string) (entities.URL, error) {
	url, err := svc.repo.GetByShortCode(shortCode)

	if err != nil {
		return entities.URL{}, err
	}

	return url, nil
}

// GetByUserId retrieves all urls for a given user
func (svc *UrlReadSvc) GetByUserId(userId string) ([]entities.URL, error) {
	if _, err := svc.userSvc.GetUserByID(userId); err != nil {
		return nil, err
	}

	urls, err := svc.repo.GetByOwner(userId)

	if err != nil {
		return nil, err
	}

	return urls, nil
}

// GetByKeyword retrieves url records given a keyword
func (svc *UrlReadSvc) GetByKeyword(keyword string) ([]entities.URL, error) {
	urls, err := svc.repo.GetByKeyword(keyword)

	if err != nil {
		return nil, err
	}

	return urls, nil
}

// GetByKeywords retrieves url records given their keywords
func (svc *UrlReadSvc) GetByKeywords(keywords []string) ([]entities.URL, error) {
	urls, err := svc.repo.GetByKeywords(keywords)

	if err != nil {
		return nil, err
	}

	return urls, nil
}

// GetByOriginalUrl retrieves a url given its original url
func (svc *UrlReadSvc) GetByOriginalUrl(originalUrl string) (entities.URL, error) {
	url, err := svc.repo.GetByOriginalUrl(originalUrl)

	if err != nil {
		return entities.URL{}, err
	}

	return url, nil
}

// GetById retrieves url given its id
func (svc *UrlReadSvc) GetById(id string) (entities.URL, error) {
	url, err := svc.repo.GetById(id)

	if err != nil {
		return entities.URL{}, err
	}

	return url, nil
}
