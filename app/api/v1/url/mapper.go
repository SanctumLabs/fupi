package url

import "github.com/sanctumlabs/curtz/app/internal/core/entities"

func mapEntityToResponseDto(url entities.URL) urlResponseDto {
	keywords := []string{}

	for _, keyword := range url.GetKeywords() {
		value := keyword.GetValue()
		if value != "" {
			keywords = append(keywords, value)
		}
	}

	return urlResponseDto{
		Id:     url.ID.String(),
		UserId: url.UserId.String(),
		urlDto: urlDto{
			OriginalUrl: url.GetOriginalURL(),
			CustomAlias: url.GetCustomAlias(),
			Keywords:    keywords,
			ExpiresOn:   url.GetExpiresOn(),
		},
		ShortCode: url.GetShortCode(),
		CreatedAt: url.CreatedAt,
		UpdatedAt: url.UpdatedAt,
		Hits:      url.GetHits(),
	}
}
