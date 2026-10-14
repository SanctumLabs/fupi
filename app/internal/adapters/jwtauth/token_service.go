// Package jwtauth adapts the pkg/jwt signer to the ports.TokenService port.
package jwtauth

import (
	"fmt"

	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/jwt"
)

type tokenService struct {
	config config.AuthConfig
	jwt    jwt.JwtGen
}

var _ ports.TokenService = (*tokenService)(nil)

// NewTokenService creates a JWT-backed TokenService with the provided configuration.
func NewTokenService(config config.AuthConfig, jwtGen jwt.JwtGen) ports.TokenService {
	return &tokenService{config: config, jwt: jwtGen}
}

func (svc *tokenService) GenerateAccessToken(userID string) (string, error) {
	token, err := svc.jwt.Encode(userID, svc.config.Secret, svc.config.Issuer, svc.config.ExpireDelta)
	if err != nil {
		return "", fmt.Errorf("failed to create access token: %w", err)
	}
	return token, nil
}

func (svc *tokenService) GenerateRefreshToken(userID string) (string, error) {
	token, err := svc.jwt.EncodeRefreshToken(userID, svc.config.Secret, svc.config.Issuer, svc.config.RefreshExpireDelta)
	if err != nil {
		return "", fmt.Errorf("failed to create refresh token: %w", err)
	}
	return token, nil
}

// Authenticate validates the token. A token that fails to decode is Unauthorized rather than a
// server error: it is caller-supplied input.
func (svc *tokenService) Authenticate(token string) (string, error) {
	// jwt.Decode's second return is the issued-at time, not the expiry its name suggests; no
	// caller needs it, so the port does not surface it.
	userID, _, err := svc.jwt.Decode(token, svc.config.Issuer, svc.config.Secret)
	if err != nil {
		return "", errdefs.Unauthorized(fmt.Errorf("%w: %w", errdefs.ErrTokenInvalid, err))
	}
	return userID, nil
}
