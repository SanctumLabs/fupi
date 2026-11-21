//go:build legacy

package auth

import (
	"errors"
	"time"

	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/pkg/jwt"
)

// AuthService represents underlying authentication implementation
type AuthService struct {
	config config.AuthConfig
	jwt    jwt.JwtGen
}

// NewService creates a new authentication service with provided configuration
func NewService(config config.AuthConfig, jwtGen jwt.JwtGen) *AuthService {
	return &AuthService{config, jwtGen}
}

// Authenticate decodes token and returns user id expiry time and error if any
func (svc *AuthService) Authenticate(token string) (string, time.Time, error) {
	return svc.jwt.Decode(token, svc.config.Jwt.Issuer, svc.config.Jwt.Secret)
}

// GenerateToken generates an access token for a provided user id
func (svc *AuthService) GenerateToken(userID string) (string, error) {
	token, err := svc.jwt.Encode(userID, svc.config.Jwt.Secret, svc.config.Jwt.Issuer, svc.config.ExpireDelta)
	if err != nil {
		return "", errors.New("failed to create access token")
	}
	return token, nil
}

// GenerateRefreshToken generates a refresh token for a provided user id
func (svc *AuthService) GenerateRefreshToken(userID string) (string, error) {
	token, err := svc.jwt.EncodeRefreshToken(userID, svc.config.Jwt.Secret, svc.config.Jwt.Issuer, svc.config.RefreshExpireDelta)
	if err != nil {
		return "", errors.New("failed to create refresh token")
	}
	return token, nil
}
