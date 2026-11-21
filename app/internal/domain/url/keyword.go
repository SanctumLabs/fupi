package url

import (
	"fmt"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

var (
	KeywordRegex = `^[a-zA-Z0-9-_]+$`
	kwRe         = regexp.MustCompile(KeywordRegex)
)

// Keyword is model for keywords attached to a url
type Keyword struct {
	identifier.ID
	Value string
}

// NewKeyword creates a new keyword
func NewKeyword(keyword string) (Keyword, error) {

	if l := len(keyword); l < 2 || l > 25 {
		return Keyword{}, fmt.Errorf("%w: '%s'", errdefs.ErrKeywordLength, keyword)
	}

	if !kwRe.MatchString(keyword) {
		return Keyword{}, fmt.Errorf("%w: '%s'", errdefs.ErrInvalidKeyword, keyword)
	}

	id := identifier.New()
	return Keyword{
		ID:    id,
		Value: keyword,
	}, nil
}

func createKeywords(keywords []string) ([]Keyword, error) {
	kws := make([]Keyword, len(keywords))

	if len(keywords) > 10 {
		return kws, errdefs.ErrKeywordsCount
	}

	for _, kw := range keywords {
		if keyword, err := NewKeyword(kw); err == nil {
			kws = append(kws, keyword)
		}
	}

	return kws, nil
}
