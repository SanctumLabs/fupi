package url

import "github.com/sanctumlabs/curtz/app/pkg/errdefs"

// OriginalURL is a value object that encapsulates validation.
// Construction fails if the URL is too short, too long, filtered, or malformed.
type OriginalURL struct {
	value string
}

// NewOriginalURL creates a new OriginalURL value object after validating the input.
func NewOriginalURL(url string) (OriginalURL, error) {
	if l := len(url); l < MinLength || l > MaxLength {
		return OriginalURL{}, errdefs.ErrInvalidURLLen
	}

	if !urlRe.MatchString(url) {
		return OriginalURL{}, errdefs.ErrInvalidURL
	}

	return OriginalURL{value: url}, nil
}

// Value returns the original URL string.
func (o OriginalURL) Value() string {
	return o.value
}
