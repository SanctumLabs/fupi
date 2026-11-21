package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTokenService accepts exactly one token and rejects everything else.
type stubTokenService struct {
	validToken string
	userID     string
}

func (s stubTokenService) GenerateAccessToken(string) (string, error)  { return "", nil }
func (s stubTokenService) GenerateRefreshToken(string) (string, error) { return "", nil }
func (s stubTokenService) Authenticate(token string) (string, error) {
	if token == s.validToken {
		return s.userID, nil
	}
	return "", errdefs.Unauthorized(errdefs.ErrTokenInvalid)
}

// newTestApp mounts the middleware ahead of a handler that echoes back whatever user id the
// middleware put on the context.
func newTestApp(config AuthConfig) *fiber.App {
	app := fiber.New()
	app.Use(AuthMiddleware(config))
	handler := func(ctx *fiber.Ctx) error {
		if userID, ok := ctx.Locals(UserIDContextKey).(string); ok {
			return ctx.SendString(userID)
		}
		return ctx.SendString("anonymous")
	}
	app.Get("/protected", handler)
	app.Get("/api/v1/curtz/auth/login", handler)
	app.Get("/health", handler)
	app.Get("/docs/v1", handler)
	app.Get("/abc123", handler)
	return app
}

func testConfig() AuthConfig {
	return AuthConfig{
		TokenService:   stubTokenService{validToken: "good-token", userID: "user-42"},
		PublicPaths:    []string{"/api/v1/curtz/auth/login", "/health"},
		PublicPrefixes: []string{"/docs/"},
	}
}

func TestAuthMiddleware_AllowsValidBearerToken(t *testing.T) {
	app := newTestApp(testConfig())

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer good-token")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestAuthMiddleware_PutsUserIDOnContext(t *testing.T) {
	app := newTestApp(testConfig())

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer good-token")

	resp, err := app.Test(req)
	require.NoError(t, err)

	body := make([]byte, resp.ContentLength)
	_, _ = resp.Body.Read(body)
	assert.Equal(t, "user-42", string(body), "the authenticated user id should be on the context")
}

func TestAuthMiddleware_AcceptsAnySchemeCasing(t *testing.T) {
	app := newTestApp(testConfig())

	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			req.Header.Set(fiber.HeaderAuthorization, scheme+" good-token")

			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		})
	}
}

func TestAuthMiddleware_Rejects(t *testing.T) {
	app := newTestApp(testConfig())

	cases := map[string]string{
		"no header":           "",
		"missing scheme":      "good-token",
		"wrong scheme":        "Basic good-token",
		"empty token":         "Bearer ",
		"unknown token":       "Bearer bad-token",
		"scheme only":         "Bearer",
		"token in wrong slot": "good-token Bearer",
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			if header != "" {
				req.Header.Set(fiber.HeaderAuthorization, header)
			}

			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestAuthMiddleware_SkipsPublicPaths(t *testing.T) {
	app := newTestApp(testConfig())

	for _, path := range []string{"/api/v1/curtz/auth/login", "/health"} {
		t.Run(path, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest("GET", path, nil))
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusOK, resp.StatusCode, "public paths need no token")
		})
	}
}

func TestAuthMiddleware_SkipsPublicPrefixes(t *testing.T) {
	app := newTestApp(testConfig())

	resp, err := app.Test(httptest.NewRequest("GET", "/docs/v1", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

// The pre-v2 regex exempted any single-segment path, which unintentionally opened up every
// top-level route. Only paths that were explicitly declared public may skip authentication.
func TestAuthMiddleware_DoesNotExemptArbitrarySingleSegmentPaths(t *testing.T) {
	app := newTestApp(testConfig())

	resp, err := app.Test(httptest.NewRequest("GET", "/abc123", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode,
		"an undeclared single-segment path must still require a token")
}
