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
func RateLimitGroups(baseURI string, redirect *regexp.Regexp, rules RateLimitRules) []servermw.RateLimitGroup {
	return []servermw.RateLimitGroup{
		{
			Name: "auth",
			Rule: rules.Auth,
			Match: func(c *fiber.Ctx) bool {
				return strings.HasPrefix(c.Path(), baseURI+"/auth/")
			},
		},
		{
			Name: "create",
			Rule: rules.Create,
			Match: func(c *fiber.Ctx) bool {
				return c.Method() == fiber.MethodPost && c.Path() == baseURI+"/urls"
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
				return c.Method() == fiber.MethodGet && redirect.MatchString(c.Path())
			},
		},
		{Name: "default", Rule: rules.Default},
	}
}
