package middleware

import (
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	servermw "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"
)

// RateLimitRules are the budgets of the route groups RateLimitGroups tells apart.
type RateLimitRules struct {
	Auth, Create, Redirect, Default servermw.RateLimitRule
}

// RateLimitGroups returns the route groups the rate limiter tells apart, most specific first: the credential endpoints
// (auth), url creation (create), the public redirect (redirect) and everything else (default). Create is limited per
// authenticated user, so the limiter must run after AuthMiddleware.
//
// Fiber routes without regard to case or a trailing slash, so the groups match the same way: a request cannot move to
// a looser group by changing the case of its path or adding a slash.
func RateLimitGroups(baseURI string, redirect *regexp.Regexp, rules RateLimitRules) []servermw.RateLimitGroup {
	authPrefix := strings.ToLower(baseURI + "/auth/")
	createPath := strings.ToLower(baseURI + "/urls")

	return []servermw.RateLimitGroup{
		{
			Name: "auth",
			Rule: rules.Auth,
			Match: func(c *fiber.Ctx) bool {
				return strings.HasPrefix(routedPath(c), authPrefix)
			},
		},
		{
			Name: "create",
			Rule: rules.Create,
			Match: func(c *fiber.Ctx) bool {
				return c.Method() == fiber.MethodPost && routedPath(c) == createPath
			},
			Key: func(c *fiber.Ctx) string {
				if userID, _ := c.Locals(UserIDContextKey).(string); userID != "" {
					return "user:" + userID
				}
				return ""
			},
		},
		{
			Name: "redirect",
			Rule: rules.Redirect,
			Match: func(c *fiber.Ctx) bool {
				return c.Method() == fiber.MethodGet && redirect.MatchString(routedPath(c))
			},
		},
		{Name: "default", Rule: rules.Default},
	}
}

// routedPath is the request path as Fiber routes it: lower case, without trailing slashes (the root stays "/").
func routedPath(c *fiber.Ctx) string {
	path := strings.TrimRight(strings.ToLower(c.Path()), "/")
	if path == "" {
		return "/"
	}
	return path
}
