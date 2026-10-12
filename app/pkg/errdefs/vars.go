package errdefs

import "errors"

const ERR_PERMANENTLY_FAILED = "PERMANENTLY_FAILED"

var (
	ErrInvalidUserId                = errors.New("user id is invalid")
	ErrInvalidUrlId                 = errors.New("url id is invalid")
	ErrInvalidCustomAlias           = errors.New("custom alias is invalid")
	ErrInvalidURL                   = errors.New("url is invalid")
	ErrFilteredURL                  = errors.New("url is not allowed")
	ErrURLAlreadyExists             = errors.New("url has already been shortened")
	ErrURLExpired                   = errors.New("url expired")
	ErrInvalidPasswordLen           = errors.New("password length is invalid")
	ErrTokenRequired                = errors.New("auth token is required")
	ErrTokenInvalid                 = errors.New("auth token is invalid")
	ErrCustomAliasInvalidLength     = errors.New("custom alias must be between 3 and 12 characters")
	ErrCustomAliasInvalidCharacters = errors.New("custom alias can only contain alphanumeric characters and dashes")
	ErrCustomAliasLikeShortCode     = errors.New("custom alias must not look like a short code: use 3 to 5 or 11 to 12 characters, or include a dash")
	ErrInvalidURLLen                = errors.New("original url length is invalid")
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
