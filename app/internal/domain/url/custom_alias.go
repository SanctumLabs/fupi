package url

import (
	"fmt"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

// CustomAlias is a value object that represents a custom alias for the URL.
// It is never generated inside the aggregate — the aggregate receives it.
type CustomAlias struct {
	value string // 3–100 chars
}

// NewCustomAlias creates a new CustomAlias value object after validating the input.
func NewCustomAlias(value string) (CustomAlias, error) {
	// Validate length
	if len(value) < 3 || len(value) > 100 {
		return CustomAlias{}, fmt.Errorf("%w: '%s'", errdefs.ErrCustomAliasInvalidLength, value)
	}

	// Validate characters (alphanumeric and dashes only)
	matched, err := regexp.MatchString(`^[a-zA-Z0-9-]+$`, value)
	if err != nil {
		return CustomAlias{}, err
	}
	if !matched {
		return CustomAlias{}, errdefs.ErrCustomAliasInvalidCharacters
	}

	return CustomAlias{value: value}, nil
}

func (ca CustomAlias) Value() string {
	return ca.value
}
