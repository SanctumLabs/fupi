package errdefs

import (
	"strings"
	"testing"
)

// Sentinels are compared with errors.Is and never formatted, so a verb in one ships to the client verbatim.
// ErrEmailInvalid and ErrUserDoestNotExist are legacy-only and still carry verbs; they are left out until the
// legacy cleanup removes or fixes them.
func TestSentinelsHaveNoFormatVerbs(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidUserId":                ErrInvalidUserId,
		"ErrInvalidUrlId":                 ErrInvalidUrlId,
		"ErrInvalidCustomAlias":           ErrInvalidCustomAlias,
		"ErrServerError":                  ErrServerError,
		"ErrInvalidURL":                   ErrInvalidURL,
		"ErrFilteredURL":                  ErrFilteredURL,
		"ErrURLAlreadyExists":             ErrURLAlreadyExists,
		"ErrURLExpired":                   ErrURLExpired,
		"ErrInvalidPasswordLen":           ErrInvalidPasswordLen,
		"ErrPasswordMissmatch":            ErrPasswordMissmatch,
		"ErrInvalidDate":                  ErrInvalidDate,
		"ErrURLAlreadyShort":              ErrURLAlreadyShort,
		"ErrNoMatchingData":               ErrNoMatchingData,
		"ErrTokenRequired":                ErrTokenRequired,
		"ErrTokenInvalid":                 ErrTokenInvalid,
		"ErrUserExists":                   ErrUserExists,
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
