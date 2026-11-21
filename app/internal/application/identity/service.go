// Package identityapp holds the Identity use cases: the orchestration between the HTTP layer,
// the identity domain, and the datastore, token and notifier ports.
package identityapp

import (
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/internal/ports"
)

// Service exposes the Identity use cases.
type Service struct {
	users     identity.UserDatastore
	tokens    ports.TokenService
	notifier  ports.Notifier
	logPrefix string
}

// NewService wires the Identity use cases to their ports.
func NewService(users identity.UserDatastore, tokens ports.TokenService, notifier ports.Notifier) *Service {
	return &Service{
		users:     users,
		tokens:    tokens,
		notifier:  notifier,
		logPrefix: "IdentityService",
	}
}

// TokenPair is the access/refresh token pair handed to an authenticated caller.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// issueTokens mints a fresh access/refresh pair for the given user id.
func (svc *Service) issueTokens(userID string) (TokenPair, error) {
	accessToken, accessErr := svc.tokens.GenerateAccessToken(userID)
	if accessErr != nil {
		return TokenPair{}, accessErr
	}

	refreshToken, refreshErr := svc.tokens.GenerateRefreshToken(userID)
	if refreshErr != nil {
		return TokenPair{}, refreshErr
	}

	return TokenPair{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}
