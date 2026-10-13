package middleware

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

const testPeer = "203.0.113.7"

// newLimitedApp builds a fiber app with the rate limiter in front of a handler answering 200 on every path.
func newLimitedApp(t *testing.T, cfg RateLimiterConfig) *fiber.App {
	t.Helper()

	handler, err := RateLimiterMiddleware(cfg)
	require.NoError(t, err)

	app := fiber.New()
	app.Use(handler)
	app.All("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	return app
}

// serve sends a GET request for path to app as if it came from the given peer address.
func serve(t *testing.T, app *fiber.App, path, peer string, headers map[string]string) *fasthttp.Response {
	t.Helper()

	var req fasthttp.Request
	req.SetRequestURI(path)
	req.Header.SetMethod(fiber.MethodGet)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	var ctx fasthttp.RequestCtx
	ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP(peer), Port: 4321}, nil)
	app.Handler()(&ctx)

	resp := &fasthttp.Response{}
	ctx.Response.CopyTo(resp)

	return resp
}

func TestRateLimiter_LimitsAfterMax(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups: []RateLimitGroup{{Name: "auth", Rule: RateLimitRule{Max: 2, Window: time.Minute}}},
	})

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())

	resp := serve(t, app, "/x", testPeer, nil)
	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode())

	var body map[string]string
	require.NoError(t, json.Unmarshal(resp.Body(), &body))
	assert.Equal(t, LimitedMessage, body["message"])
	assert.Equal(t, "too many requests", body["message"])

	retryAfter, err := strconv.Atoi(string(resp.Header.Peek(fiber.HeaderRetryAfter)))
	require.NoError(t, err)
	assert.Positive(t, retryAfter)
	assert.Equal(t, "2", string(resp.Header.Peek("X-RateLimit-Limit")))
}

func TestRateLimiter_ExemptPathsAreNeverLimited(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups:         []RateLimitGroup{{Name: "default", Rule: RateLimitRule{Max: 1, Window: time.Minute}}},
		ExemptPaths:    []string{"/health"},
		ExemptPrefixes: []string{"/docs"},
	})

	for _, path := range []string{"/health", "/docs/index.html"} {
		for i := 0; i < 5; i++ {
			resp := serve(t, app, path, testPeer, nil)
			assert.Equal(t, fiber.StatusOK, resp.StatusCode(), "%s request %d", path, i+1)
			assert.Empty(t, resp.Header.Peek("X-RateLimit-Limit"), path)
		}
	}
}

func TestRateLimiter_GroupsHaveSeparateBuckets(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups: []RateLimitGroup{
			{
				Name:  "a",
				Rule:  RateLimitRule{Max: 1, Window: time.Minute},
				Match: func(c *fiber.Ctx) bool { return c.Path() == "/a" },
			},
			{Name: "b", Rule: RateLimitRule{Max: 1, Window: time.Minute}},
		},
	})

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/a", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/b", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/a", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/b", testPeer, nil).StatusCode())
}

func TestRateLimiter_NoMatchingGroupPassesUnlimited(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups: []RateLimitGroup{{
			Name:  "a",
			Rule:  RateLimitRule{Max: 1, Window: time.Minute},
			Match: func(c *fiber.Ctx) bool { return c.Path() == "/a" },
		}},
	})

	for i := 0; i < 3; i++ {
		resp := serve(t, app, "/other", testPeer, nil)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode())
		assert.Empty(t, resp.Header.Peek("X-RateLimit-Limit"))
	}
}

func TestRateLimiter_KeysSeparateClients(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups: []RateLimitGroup{{Name: "default", Rule: RateLimitRule{Max: 1, Window: time.Minute}}},
	})

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", "203.0.113.7", nil).StatusCode())
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", "203.0.113.8", nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/x", "203.0.113.7", nil).StatusCode())
}

func TestRateLimiter_GroupKeyFunc(t *testing.T) {
	app := newLimitedApp(t, RateLimiterConfig{
		Groups: []RateLimitGroup{{
			Name: "default",
			Rule: RateLimitRule{Max: 1, Window: time.Minute},
			Key: func(c *fiber.Ctx) string {
				if user := c.Get("X-User"); user != "" {
					return "user:" + user
				}
				return ""
			},
		}},
	})

	u1 := map[string]string{"X-User": "u1"}
	u2 := map[string]string{"X-User": "u2"}

	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, u1).StatusCode())
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, u2).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/x", testPeer, u1).StatusCode())

	// Without a user the request falls back to the IP bucket, which is still unused.
	assert.Equal(t, fiber.StatusOK, serve(t, app, "/x", testPeer, nil).StatusCode())
	assert.Equal(t, fiber.StatusTooManyRequests, serve(t, app, "/x", testPeer, nil).StatusCode())
}
