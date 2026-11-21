// Package middleware holds HTTP middleware for the Curtz API.
package middleware

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/curtz/app/internal/ports"
)

// UserIDContextKey is where the authenticated caller's user id is stored on the request context.
const UserIDContextKey = "userId"

// AuthConfig configures the bearer-token middleware.
type AuthConfig struct {
	// TokenService validates the bearer token
	TokenService ports.TokenService

	// PublicPaths are exact request paths that skip authentication entirely, such as the
	// register, login and verify endpoints, health checks and docs.
	PublicPaths []string

	// PublicPrefixes are path prefixes that skip authentication, for whole open subtrees.
	PublicPrefixes []string
}

// AuthMiddleware authenticates requests carrying a `Authorization: Bearer <token>` header and
// puts the caller's user id on the context under UserIDContextKey.
//
// Paths are matched exactly (or by prefix) rather than by regular expression: the pre-v2
// middleware used a regex whose client pattern also matched any single-segment path, which
// silently exempted far more than intended.
func AuthMiddleware(config AuthConfig) fiber.Handler {
	public := make(map[string]struct{}, len(config.PublicPaths))
	for _, path := range config.PublicPaths {
		public[path] = struct{}{}
	}

	return func(ctx *fiber.Ctx) error {
		path := ctx.Path()

		if _, ok := public[path]; ok {
			return ctx.Next()
		}
		for _, prefix := range config.PublicPrefixes {
			if strings.HasPrefix(path, prefix) {
				return ctx.Next()
			}
		}

		authHeader := ctx.Get(fiber.HeaderAuthorization)
		if authHeader == "" {
			slog.WarnContext(ctx.UserContext(), "request rejected: no Authorization header", "path", path)
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		scheme, token, found := strings.Cut(authHeader, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
			slog.WarnContext(ctx.UserContext(), "request rejected: Authorization header is not a bearer token", "path", path)
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		userID, err := config.TokenService.Authenticate(token)
		if err != nil {
			slog.WarnContext(ctx.UserContext(), "request rejected: invalid bearer token", "path", path, "error", err)
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		ctx.Locals(UserIDContextKey, userID)
		return ctx.Next()
	}
}
