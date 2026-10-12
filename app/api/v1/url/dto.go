package urlapi

import "time"

type createUrlRequestDto struct {
	OriginalUrl string     `json:"original_url" validate:"required"`
	CustomAlias string     `json:"custom_alias"`
	ExpiresOn   *time.Time `json:"expires_on" validate:"required"`
	Keywords    []string   `json:"keywords"`
}

// updateUrlRequestDto carries the fields a PATCH may change. An absent field is left as it is; "keywords": [] clears
// the keywords.
type updateUrlRequestDto struct {
	ExpiresOn *time.Time `json:"expires_on"`
	Keywords  *[]string  `json:"keywords"`
}

type urlResponseDto struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	ShortCode   string    `json:"short_code"`
	ShortUrl    string    `json:"short_url"`
	CustomAlias string    `json:"custom_alias,omitempty"`
	OriginalUrl string    `json:"original_url"`
	Status      string    `json:"status"`
	ExpiresOn   time.Time `json:"expires_on"`
	Keywords    []string  `json:"keywords"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type urlListResponseDto struct {
	Records []urlResponseDto `json:"records"`
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	Size    int              `json:"size"`
}

// errorResponseDto is the single error shape returned by every url endpoint.
type errorResponseDto struct {
	Message string `json:"message"`
}
