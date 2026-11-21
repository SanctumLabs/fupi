//go:build legacy

package url

import "time"

// urlDto is response for a given url.This contains common fields for all other url dtos
type urlDto struct {
	// OriginalUrl is the original url to shorten
	OriginalUrl string `json:"original_url" binding:"required"`

	// CustomAlias is the custom alias for the url provided for by the user
	CustomAlias string `json:"custom_alias" default:""`

	// ExpiresOn is the expiration date for the url
	ExpiresOn time.Time `json:"expires_on" binding:"required"`

	// Keywords is the list of keywords to be attached to the url
	Keywords []string `json:"keywords"`
}

//urlResponseDto is the response dto for the url service
type urlResponseDto struct {
	urlDto
	// Id is the id of the created url to shorten
	Id string `json:"id"`

	// UserId is the id of the user whow created this url to shorten
	UserId string `json:"user_id"`

	// ShortCode is the short code generated for the url provided for by the user
	ShortCode string `json:"short_code"`

	// CreatedAt is the created date for the url
	CreatedAt time.Time `json:"created_at" default:""`

	// UpdatedAt is the updated date for the url
	UpdatedAt time.Time `json:"updated_at" default:""`

	// Hits is the number of hits of the url
	Hits uint `json:"hits"`
}

// CreateUrlDto is the request body for creating a url
type createShortUrlDto struct {
	urlDto
}

// updateUrlDto is the request body for creating a url
type updateShortUrlDto struct {
	// CustomAlias is the custom alias for the url provided for by the user
	CustomAlias string `json:"custom_alias,omitempty"`

	// ExpiresOn is the expiration date for the url
	ExpiresOn *time.Time `json:"expires_on,omitempty"`

	// Keywords is the list of keywords to be attached to the url
	Keywords []string `json:"keywords,omitempty"`
}
