package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/api/probes"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

// Postgres is the one required dependency: with it unreachable the process must exit with an error, not hang or serve.
func TestRun_ReturnsAnErrorWhenPostgresIsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about three seconds of connection retries")
	}
	cfg, err := config.Load(lookupOf(map[string]string{
		"DATABASE_PORT": "1", "DATABASE_CONN_TIMEOUT": "1", "REDIS_ADDRESS": "127.0.0.1:1",
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = run(ctx, cfg)

	require.Error(t, err)
	assert.ErrorContains(t, err, "postgres")
}

// healthServer answers GET /health with status and every other path with 404, like the API's liveness route.
func healthServer(t *testing.T, status int) (host, port string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != probes.LivePath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	return host, port
}

func probeEnv(host, port string) config.Lookup {
	return lookupOf(map[string]string{"SERVER_HOST": host, "HTTP_PORT": port})
}

func TestHealthcheck_ExitsZeroWhenTheAPIIsAlive(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "ok")
}

func TestHealthcheck_ExitsOneOnAnUnhealthyAnswer(t *testing.T) {
	host, port := healthServer(t, http.StatusServiceUnavailable)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "503")
}

func TestHealthcheck_ExitsOneWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	require.NoError(t, ln.Close())
	var out bytes.Buffer

	code := healthcheck(probeEnv("127.0.0.1", port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "healthcheck:")
}

func TestHealthcheck_GivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	previous := healthcheckTimeout
	healthcheckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { healthcheckTimeout = previous })
	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	var out bytes.Buffer

	start := time.Now()
	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Less(t, time.Since(start), 2*time.Second)
}

// A wildcard bind address cannot be dialed, so the probe goes through loopback; any other host is dialed as configured.
func TestHealthcheck_ReachesAWildcardBindThroughLoopback(t *testing.T) {
	_, port := healthServer(t, http.StatusOK)

	for _, host := range []string{"0.0.0.0", "::", ""} {
		var out bytes.Buffer
		code := healthcheck(probeEnv(host, port), &out)
		assert.Equal(t, 0, code, "SERVER_HOST=%q: %s", host, out.String())
	}
}

func TestProbeHost(t *testing.T) {
	for host, want := range map[string]string{
		"": "127.0.0.1", "0.0.0.0": "127.0.0.1", "::": "127.0.0.1",
		"127.0.0.1": "127.0.0.1", "10.1.2.3": "10.1.2.3", "localhost": "localhost",
	} {
		assert.Equal(t, want, probeHost(host), "host %q", host)
	}
}

func TestHealthcheck_RejectsAnInvalidPort(t *testing.T) {
	var out bytes.Buffer

	code := healthcheck(lookupOf(map[string]string{"HTTP_PORT": "abc"}), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "HTTP_PORT")
}

// The docs are exempt from the limiter, but a short code that merely starts with "docs" is a redirect like any other.
func TestNewRateLimiter_ExemptsTheDocsButNotCodesStartingWithDocs(t *testing.T) {
	one := config.RateLimitRule{Max: 1, Window: time.Minute}
	limit, err := newRateLimiter(config.App{RateLimit: config.RateLimitSettings{
		Auth: one, Create: one, Redirect: one, Default: one,
		RedisTimeout: time.Second, RedisRetryAfter: time.Second,
	}}, nil)
	require.NoError(t, err)

	app := fiber.New()
	app.Use(limit)
	app.Use(func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	statuses := func(path string) []int {
		var got []int
		for range 3 {
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
			require.NoError(t, err)
			got = append(got, resp.StatusCode)
		}
		return got
	}

	for _, path := range []string{"/docs", "/docs/", "/docs/swagger.json", probes.LivePath, probes.ReadyPath} {
		assert.Equal(t, []int{200, 200, 200}, statuses(path), path)
	}
	assert.Equal(t, []int{200, 429, 429}, statuses("/docs0000"))
}
