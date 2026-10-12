package urlapi

import (
	"strings"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
)

// toUrlResponse maps a URL aggregate onto the wire representation. short_url is built on the short code, which every
// url has; the custom alias, when set, redirects too.
func toUrlResponse(shortBaseURL string, u url.URL) urlResponseDto {
	keywords := make([]string, 0, len(u.Keywords()))
	for _, keyword := range u.Keywords() {
		keywords = append(keywords, keyword.Value)
	}

	shortCode := u.ShortCode().Value()
	return urlResponseDto{
		ID:          entity.IDToString(u.ID()),
		UserID:      entity.IDToString(u.UserId()),
		ShortCode:   shortCode,
		ShortUrl:    strings.TrimSuffix(shortBaseURL, "/") + "/" + shortCode,
		CustomAlias: u.CustomAlias().Value(),
		OriginalUrl: u.OriginalURL().Value(),
		Status:      string(u.Status()),
		ExpiresOn:   u.ExpiresOn(),
		Keywords:    keywords,
		CreatedAt:   u.CreatedAt(),
		UpdatedAt:   u.UpdatedAt(),
	}
}
