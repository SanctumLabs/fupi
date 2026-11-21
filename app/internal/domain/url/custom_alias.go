package url

import (
	"fmt"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

var customAliasRe = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// CustomAlias is an optional value object that represents a user-chosen alias for the URL.
// The zero value means "no alias". Once assigned to a URL it is immutable.
type CustomAlias struct {
	value string // 3–12 chars
}

// NewCustomAlias creates a new CustomAlias value object after validating the input.
func NewCustomAlias(value string) (CustomAlias, error) {
	if len(value) < 3 || len(value) > 12 {
		return CustomAlias{}, fmt.Errorf("%w: '%s'", errdefs.ErrCustomAliasInvalidLength, value)
	}

	if !customAliasRe.MatchString(value) {
		return CustomAlias{}, fmt.Errorf("%w: '%s'", errdefs.ErrCustomAliasInvalidCharacters, value)
	}

	return CustomAlias{value: value}, nil
}

func (ca CustomAlias) Value() string {
	return ca.value
}

// IsZero reports whether no alias has been set
func (ca CustomAlias) IsZero() bool {
	return ca.value == ""
}
