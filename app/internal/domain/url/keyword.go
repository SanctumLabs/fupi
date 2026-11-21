package url

import (
	"fmt"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

// MaxKeywords is the maximum number of keywords a URL may carry
const MaxKeywords = 10

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

	if l := len(keyword); l < 2 || l > 100 {
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
	if len(keywords) > MaxKeywords {
		return nil, errdefs.ErrKeywordsCount
	}

	kws := make([]Keyword, 0, len(keywords))
	for _, kw := range keywords {
		keyword, err := NewKeyword(kw)
		if err != nil {
			return nil, err
		}
		kws = append(kws, keyword)
	}

	return kws, nil
}
