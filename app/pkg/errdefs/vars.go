package errdefs

import "errors"

const ERR_PERMANENTLY_FAILED = "PERMANENTLY_FAILED"

var (
	ErrInvalidUserId                = errors.New("user id is invalid")
	ErrInvalidUrlId                 = errors.New("url id is invalid")
	ErrInvalidCustomAlias           = errors.New("custom alias '%s' is invalid")
	ErrInvalidURL                   = errors.New("url '%s' is invalid")
	ErrFilteredURL                  = errors.New("url '%s' matches filter pattern")
	ErrURLAlreadyExists             = errors.New("url '%s' has already been shortened")
	ErrURLNotFound                  = errors.New("url '%s' not found")
	ErrURLExpired                   = errors.New("url expired")
	ErrInvalidPasswordLen           = errors.New("password length is invalid")
	ErrShortCodeEmpty               = errors.New("short_code '%s' must not be empty")
	ErrNoShortCode                  = errors.New("short_code %s' is not found")
	ErrTokenRequired                = errors.New("auth token is required")
	ErrTokenInvalid                 = errors.New("auth token is invalid")
	ErrCustomAliasInvalidLength     = errors.New("custom alias must be between 3 and 12 characters")
	ErrCustomAliasInvalidCharacters = errors.New("custom alias can only contain alphanumeric characters and dashes")
	ErrInvalidURLLen                = errors.New("original URL '%s' must be between 5 and 2048 characters")
	ErrPastExpiration               = errors.New("expiration date must be in the future")
	ErrKeywordsCount                = errors.New("cannot have more than 10 keywords")
	ErrKeywordLength                = errors.New("keyword must be between 2 and 100 characters")
	ErrInvalidKeyword               = errors.New("keyword can only contain alphanumeric characters, dashes, and underscores")
	ErrShortCodeInvalidLength       = errors.New("short code must be between 6 and 10 characters")
	ErrShortCodeInvalidCharacters   = errors.New("short code can only contain alphanumeric characters")
	ErrURLNotActive                 = errors.New("url is not active")
	ErrUserAlreadyVerified          = errors.New("user is already verified")
	ErrVerificationTokenInvalid     = errors.New("verification token is invalid")
	ErrVerificationTokenExpired     = errors.New("verification token has expired")
	ErrInvalidCredentials           = errors.New("invalid email or password")
	ErrInvalidUserStatusTransition  = errors.New("invalid user status transition")
	ErrInvalidStatusTransition      = errors.New("invalid url status transition")
)

type Error struct {
	msg string
}

func (e *Error) Error() string {
	return e.msg
}

func NewError(msg string) Error {
	return Error{msg}
}
