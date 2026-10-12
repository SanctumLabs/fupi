package errdefs

import (
	"strings"
	"testing"
)

// Sentinels are compared with errors.Is and never formatted, so a verb in one ships to the client verbatim.
func TestSentinelsHaveNoFormatVerbs(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidUserId":                ErrInvalidUserId,
		"ErrInvalidUrlId":                 ErrInvalidUrlId,
		"ErrInvalidCustomAlias":           ErrInvalidCustomAlias,
		"ErrInvalidURL":                   ErrInvalidURL,
		"ErrFilteredURL":                  ErrFilteredURL,
		"ErrURLAlreadyExists":             ErrURLAlreadyExists,
		"ErrURLExpired":                   ErrURLExpired,
		"ErrInvalidPasswordLen":           ErrInvalidPasswordLen,
		"ErrTokenRequired":                ErrTokenRequired,
		"ErrTokenInvalid":                 ErrTokenInvalid,
		"ErrCustomAliasInvalidLength":     ErrCustomAliasInvalidLength,
		"ErrCustomAliasInvalidCharacters": ErrCustomAliasInvalidCharacters,
		"ErrInvalidURLLen":                ErrInvalidURLLen,
		"ErrPastExpiration":               ErrPastExpiration,
		"ErrKeywordsCount":                ErrKeywordsCount,
		"ErrKeywordLength":                ErrKeywordLength,
		"ErrInvalidKeyword":               ErrInvalidKeyword,
		"ErrShortCodeInvalidLength":       ErrShortCodeInvalidLength,
		"ErrShortCodeInvalidCharacters":   ErrShortCodeInvalidCharacters,
		"ErrURLNotActive":                 ErrURLNotActive,
		"ErrUserAlreadyVerified":          ErrUserAlreadyVerified,
		"ErrVerificationTokenInvalid":     ErrVerificationTokenInvalid,
		"ErrVerificationTokenExpired":     ErrVerificationTokenExpired,
		"ErrInvalidCredentials":           ErrInvalidCredentials,
		"ErrInvalidUserStatusTransition":  ErrInvalidUserStatusTransition,
		"ErrInvalidStatusTransition":      ErrInvalidStatusTransition,
	}

	for name, err := range sentinels {
		if strings.Contains(err.Error(), "%") {
			t.Errorf("%s = %q contains a format verb; add context by wrapping at the call site", name, err)
		}
	}
}
