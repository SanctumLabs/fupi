//go:build legacy

package contracts

import (
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/validators"
)

type UpdateUrlRequest struct {
	UserId      string
	UrlId       string
	CustomAlias string
	Keywords    []string
	ExpiresOn   *time.Time
}

func NewUpdateUrlRequest(userId, urlId, customAlias string, keywords []string, expiresOn *time.Time) (UpdateUrlRequest, error) {
	if err := validators.IsValidUserId(userId); err != nil {
		return UpdateUrlRequest{}, err
	}

	if err := validators.IsValidUrlId(urlId); err != nil {
		return UpdateUrlRequest{}, err
	}

	if len(customAlias) != 0 {
		if err := validators.IsValidCustomAlias(customAlias); err != nil {
			return UpdateUrlRequest{}, err
		}
	}

	if expiresOn != nil {
		if err := validators.IsValidExpirationTime(*expiresOn); err != nil {
			return UpdateUrlRequest{}, err
		}
	}

	return UpdateUrlRequest{
		UserId:      userId,
		UrlId:       urlId,
		CustomAlias: customAlias,
		Keywords:    keywords,
		ExpiresOn:   expiresOn,
	}, nil
}
