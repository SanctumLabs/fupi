package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	servermw "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBaseURI = "/api/v1/fupi"

var testRedirectPattern = regexp.MustCompile(`^/[a-zA-Z0-9-]{3,12}$`)

func testRules() RateLimitRules {
	return RateLimitRules{
		Auth:     servermw.RateLimitRule{Max: 10, Window: time.Minute},
		Create:   servermw.RateLimitRule{Max: 30, Window: time.Minute},
		Redirect: servermw.RateLimitRule{Max: 300, Window: time.Minute},
		Default:  servermw.RateLimitRule{Max: 100, Window: time.Minute},
	}
}

func TestRateLimitGroups_Routing(t *testing.T) {
	groups := RateLimitGroups(testBaseURI, testRedirectPattern, testRules())

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		for _, group := range groups {
			if group.Match == nil || group.Match(c) {
				return c.SendString(group.Name)
			}
		}
		return c.SendString("none")
	})

	tests := []struct {
		method, path, want string
	}{
		{http.MethodPost, testBaseURI + "/auth/login", "auth"},
		{http.MethodGet, testBaseURI + "/auth/verify", "auth"},
		{http.MethodPost, testBaseURI + "/urls", "create"},
		{http.MethodGet, testBaseURI + "/urls", "default"},
		{http.MethodGet, "/abc123", "redirect"},
		{http.MethodPost, "/abc123", "default"},
		// /docs is shaped like a short code. It is never limited at all: the middleware exempts it before any group is asked.
		{http.MethodGet, "/docs", "redirect"},
		{http.MethodGet, testBaseURI + "/urls/xyz", "default"},
		// Fiber routes case- and trailing-slash-insensitively, so the groups must match the same way.
		{http.MethodPost, testBaseURI + "/urls/", "create"},
		{http.MethodPost, "/API/V1/FUPI/URLS", "create"},
		{http.MethodPost, testBaseURI + "/auth/login/", "auth"},
		{http.MethodPost, "/API/v1/fupi/AUTH/login", "auth"},
		{http.MethodGet, "/ABC123/", "redirect"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
			require.NoError(t, err)
			body := make([]byte, 16)
			n, _ := resp.Body.Read(body)
			assert.Equal(t, tc.want, string(body[:n]))
		})
	}
}

func TestRateLimitGroups_CarryTheirRules(t *testing.T) {
	rules := testRules()
	groups := RateLimitGroups(testBaseURI, testRedirectPattern, rules)

	got := map[string]servermw.RateLimitRule{}
	for _, group := range groups {
		got[group.Name] = group.Rule
	}
	assert.Equal(t, map[string]servermw.RateLimitRule{
		"auth": rules.Auth, "create": rules.Create, "redirect": rules.Redirect, "default": rules.Default,
	}, got)
}

func TestRateLimitGroups_CreateKeysOnUser(t *testing.T) {
	groups := RateLimitGroups(testBaseURI, testRedirectPattern, testRules())
	var create servermw.RateLimitGroup
	for _, group := range groups {
		if group.Name == "create" {
			create = group
		}
	}
	require.NotNil(t, create.Key)

	app := fiber.New()
	app.Get("/with", func(c *fiber.Ctx) error {
		c.Locals(UserIDContextKey, "u-1")
		return c.SendString(create.Key(c))
	})
	app.Get("/without", func(c *fiber.Ctx) error {
		return c.SendString(create.Key(c))
	})

	for path, want := range map[string]string{"/with": "user:u-1", "/without": ""} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		require.NoError(t, err)
		body := make([]byte, 16)
		n, _ := resp.Body.Read(body)
		assert.Equal(t, want, string(body[:n]), path)
	}
}
