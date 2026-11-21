package jwtauth

import (
	"testing"

	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/ports"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() config.AuthConfig {
	return config.AuthConfig{
		Jwt: config.Jwt{
			Secret:             "test-secret",
			Issuer:             "curtz-test",
			ExpireDelta:        15,
			RefreshExpireDelta: 24,
		},
	}
}

func TestTokenService_AccessTokenRoundTrip(t *testing.T) {
	svc := NewTokenService(testConfig(), jwt.New())
	userID := entity.IDToString(entity.NewID())

	token, err := svc.GenerateAccessToken(userID)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	gotUserID, err := svc.Authenticate(token)
	require.NoError(t, err)
	assert.Equal(t, userID, gotUserID)
}

func TestTokenService_RefreshTokenRoundTrip(t *testing.T) {
	svc := NewTokenService(testConfig(), jwt.New())
	userID := entity.IDToString(entity.NewID())

	token, err := svc.GenerateRefreshToken(userID)
	require.NoError(t, err)

	gotUserID, err := svc.Authenticate(token)
	require.NoError(t, err)
	assert.Equal(t, userID, gotUserID)
}

func TestTokenService_AccessAndRefreshTokensDiffer(t *testing.T) {
	svc := NewTokenService(testConfig(), jwt.New())
	userID := entity.IDToString(entity.NewID())

	access, err := svc.GenerateAccessToken(userID)
	require.NoError(t, err)
	refresh, err := svc.GenerateRefreshToken(userID)
	require.NoError(t, err)

	assert.NotEqual(t, access, refresh, "refresh token must not be identical to the access token")
}

func TestTokenService_Authenticate_Rejects(t *testing.T) {
	svc := NewTokenService(testConfig(), jwt.New())
	userID := entity.IDToString(entity.NewID())

	validToken, err := svc.GenerateAccessToken(userID)
	require.NoError(t, err)

	otherIssuer := testConfig()
	otherIssuer.Jwt.Issuer = "someone-else"

	otherSecret := testConfig()
	otherSecret.Jwt.Secret = "a-different-secret"

	cases := map[string]struct {
		svc   ports.TokenService
		token string
	}{
		"garbage token":     {svc, "not-a-jwt"},
		"empty token":       {svc, ""},
		"wrong issuer":      {NewTokenService(otherIssuer, jwt.New()), validToken},
		"wrong signing key": {NewTokenService(otherSecret, jwt.New()), validToken},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.svc.Authenticate(tc.token)
			require.Error(t, err)
			assert.True(t, errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
		})
	}
}

func TestTokenService_Authenticate_RejectsExpiredToken(t *testing.T) {
	expired := testConfig()
	expired.Jwt.ExpireDelta = -1 // already past

	svc := NewTokenService(expired, jwt.New())
	token, err := svc.GenerateAccessToken(entity.IDToString(entity.NewID()))
	require.NoError(t, err)

	_, err = svc.Authenticate(token)
	require.Error(t, err)
	assert.True(t, errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
}
