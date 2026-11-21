package ports

// TokenService issues and validates the bearer tokens used to authenticate API callers.
type TokenService interface {
	// GenerateAccessToken issues a short-lived access token for the given user id
	GenerateAccessToken(userID string) (string, error)

	// GenerateRefreshToken issues a long-lived refresh token for the given user id
	GenerateRefreshToken(userID string) (string, error)

	// Authenticate validates a token and returns the user id it was issued for
	Authenticate(token string) (string, error)
}
