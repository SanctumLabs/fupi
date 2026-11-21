package write

import (
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/pkg/validators"
)

//UrlSvc represents a url service use case
type UrlWriteSvc struct {
	// repo is an interface used to perform CUD operations on URL records. Does not perform write operations
	repo contracts.UrlWriteRepository
	// userSvc is an interface used to interact with the user service use cases
	userSvc contracts.UserService
}

// NewUrlSvc creates a new url service
func NewUrlWriteSvc(urlRepository contracts.UrlWriteRepository, userSvc contracts.UserService) *UrlWriteSvc {
	return &UrlWriteSvc{urlRepository, userSvc}
}

// CreateUrl creates a new shorted url given a user id, original url, custom alias, when it should expire and slice of keywords
func (svc *UrlWriteSvc) CreateUrl(userId string, originalUrl string, customAlias string, expiresOn time.Time, keywords []string) (entities.URL, error) {
	if _, err := svc.userSvc.GetUserByID(userId); err != nil {
		return entities.URL{}, err
	}

	userIdentifier := identifier.New().FromString(userId)
	url, err := entities.NewUrl(userIdentifier, originalUrl, customAlias, expiresOn, keywords)

	if err != nil {
		return entities.URL{}, err
	}

	return svc.repo.Save(*url)
}

// UpdateUrl performs an update on an existing shortened URL
func (svc *UrlWriteSvc) UpdateUrl(command contracts.UpdateUrlRequest) (entities.URL, error) {
	userID := command.UserId
	expiresOn := command.ExpiresOn
	keywords := command.Keywords
	customAlias := command.CustomAlias
	urlID := command.UrlId

	if _, err := svc.userSvc.GetUserByID(userID); err != nil {
		return entities.URL{}, err
	}

	if expiresOn != nil {
		if err := validators.IsValidExpirationTime(*expiresOn); err != nil {
			return entities.URL{}, err
		}
	}

	kws := make([]entities.Keyword, len(keywords))
	if len(keywords) != 0 {
		for i, keyword := range keywords {
			if kw, err := entities.NewKeyword(keyword); err == nil {
				kws[i] = *kw
			} else {
				return entities.URL{}, err
			}
		}
	}

	updatedUrl, err := svc.repo.Update(urlID, customAlias, kws, expiresOn)
	if err != nil {
		return entities.URL{}, err
	}

	return updatedUrl, nil
}

// Remove removes a saved shortened url given its ID
func (svc *UrlWriteSvc) Remove(id string) error {
	err := svc.repo.Delete(id)

	if err != nil {
		return err
	}
	return nil
}
