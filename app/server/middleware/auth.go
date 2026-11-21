//go:build legacy

package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/internal/services/auth"
	"github.com/sanctumlabs/curtz/app/tools/logger"
)

var (
	healthRegex = regexp.MustCompile("^(/health)$")
	authRegex   = regexp.MustCompile("^/api/v[0-9]+/curtz/auth/(register|login|oauth/token)$")
	clientRegex = regexp.MustCompile(`^/[a-zA-Z0-9]+$|/auth/verify(/\?v=[a-zA-Z0-9]+)*|/docs/(/\?v=[a-zA-Z0-9]+)*`)
)

// NewAuthMiddleware creates a new auth middleware for authenticating requests
func NewAuthMiddleware(config config.AuthConfig, authService *auth.AuthService) Middleware {
	log := logger.NewLogger("auth")
	log.EnableJSONOutput(true)

	return func(context *gin.Context) {
		authHeader := context.GetHeader("Authorization")

		requestUrl := context.Request.URL

		requestPath := requestUrl.Path

		// if request path matches register url, then allow request through
		if authRegex.MatchString(requestPath) {
			context.Next()
			return
		}

		if healthRegex.MatchString(requestPath) {
			context.Next()
			return
		}

		if clientRegex.MatchString(requestPath) {
			context.Next()
			return
		}

		if authHeader == "" {
			log.Error("No Authorization header found")
			context.AbortWithStatus(http.StatusUnauthorized)
			return
		} else {
			if len(authHeader) < 7 || strings.ToUpper(authHeader[:6]) != "BEARER" {
				log.Error("Authorization header is not a bearer token")
				context.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			token := authHeader[7:]

			userId, _, err := authService.Authenticate(token)
			if err != nil {
				log.Error(err)
				context.AbortWithStatus(http.StatusUnauthorized)
				return
			}

			context.Set("userId", userId)
			context.Next()
		}
	}
}
