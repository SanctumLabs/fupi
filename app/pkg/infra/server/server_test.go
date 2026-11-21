package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/gofiber/fiber/v2"
	servermw "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generated spec is embedded, so the docs are served whatever the working directory is.
func TestNewServer_ServesTheEmbeddedOpenAPISpec(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "fupi-test"})

	for path, want := range map[string]string{"/docs/": "text/html", "/docs/swagger.json": "application/json"} {
		resp, err := srv.App().Test(httptest.NewRequest(http.MethodGet, path, nil))
		require.NoError(t, err, path)
		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
		assert.Contains(t, resp.Header.Get("Content-Type"), want, path)
	}
}

type stubRouter struct{ routes []router.Route }

func (s stubRouter) Routes() []router.Route { return s.routes }

func TestServer_RegisterHandlers(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "fupi-test"})
	srv.RegisterHandlers([]router.Router{
		stubRouter{routes: []router.Route{
			router.NewGetRoute("/ping", func(ctx *fiber.Ctx) error {
				return ctx.SendString("pong")
			}),
		}},
	})

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/ping", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestServer_UseRegistersMiddleware(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "fupi-test"})

	called := false
	srv.Use(func(ctx *fiber.Ctx) error {
		called = true
		return ctx.Next()
	})
	srv.RegisterHandlers([]router.Router{
		stubRouter{routes: []router.Route{
			router.NewGetRoute("/ping", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusNoContent) }),
		}},
	})

	_, err := srv.App().Test(httptest.NewRequest("GET", "/ping", nil))
	require.NoError(t, err)
	assert.True(t, called, "registered middleware should run for a request")
}

func listenOnFreePort(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return ln
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Cancelling the context calls onDrain first, lets the in-flight request finish, and only then returns.
func TestServe_FinishesInFlightRequestsBeforeReturning(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})
	started := make(chan struct{})
	release := make(chan struct{})
	srv.RegisterHandlers([]router.Router{stubRouter{routes: []router.Route{
		router.NewGetRoute("/slow", func(c *fiber.Ctx) error {
			close(started)
			<-release
			return c.SendString("done")
		}),
	}}})
	ln := listenOnFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var drained atomic.Bool
	served := make(chan error, 1)
	go func() {
		served <- srv.ServeListener(ctx, ln, 5*time.Second, func() { drained.Store(true) })
	}()

	type response struct {
		body string
		err  error
	}
	responses := make(chan response, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			responses <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		responses <- response{body: string(body)}
	}()

	waitFor(t, started, "the request to reach its handler")
	cancel() // the SIGTERM

	require.Eventually(t, drained.Load, 2*time.Second, 5*time.Millisecond, "onDrain must run when the context is cancelled")
	select {
	case err := <-served:
		t.Fatalf("Serve returned (%v) while a request was still in flight", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	got := <-responses
	require.NoError(t, got.err)
	assert.Equal(t, "done", got.body)
	require.NoError(t, <-served)
}

// A request that never finishes must not hold the process forever: shutdown gives up at the timeout and says so.
func TestServe_ReturnsAnErrorWhenInFlightRequestsOutlastTheTimeout(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv.RegisterHandlers([]router.Router{stubRouter{routes: []router.Route{
		router.NewGetRoute("/stuck", func(c *fiber.Ctx) error {
			close(started)
			<-release
			return nil
		}),
	}}})
	ln := listenOnFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- srv.ServeListener(ctx, ln, 100*time.Millisecond, nil) }()
	go func() { _, _ = http.Get("http://" + ln.Addr().String() + "/stuck") }()

	waitFor(t, started, "the request to reach its handler")
	cancel()

	select {
	case err := <-served:
		assert.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not give up at the shutdown timeout")
	}
}

// failingListener accepts nothing: every Accept fails with a permanent error, as a broken socket would.
type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

// fasthttp reports a listener that was merely closed as a clean stop, so a listener failure has to be a permanent
// accept error to reach the caller.
func TestServe_ReturnsTheListenerError(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})
	ln := listenOnFreePort(t)
	t.Cleanup(func() { _ = ln.Close() })

	err := srv.ServeListener(context.Background(), failingListener{ln}, time.Second, nil)

	require.Error(t, err)
	assert.ErrorContains(t, err, "accept failed")
}

// lockedBuffer lets a test read what other goroutines logged.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The shutdown must be visible in the application log (slog, which the log pipeline ships), not only in the server's
// own logger.
func TestServe_LogsTheShutdown(t *testing.T) {
	out := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	srv := NewServer(ServerConfig{AppName: "fupi-test"})
	srv.RegisterHandlers([]router.Router{stubRouter{routes: []router.Route{
		router.NewGetRoute("/ping", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }),
	}}})
	ln := listenOnFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- srv.ServeListener(ctx, ln, time.Second, nil) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + ln.Addr().String() + "/ping")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusNoContent
	}, 5*time.Second, 20*time.Millisecond, "the server never started serving")

	cancel()
	require.NoError(t, <-served)

	assert.Contains(t, out.String(), "shutting down server")
}

// A context that is already cancelled (a SIGTERM during startup) must not leave a server accepting connections behind
// a Serve that has returned: the caller is about to close the databases.
func TestServe_AContextCancelledBeforeServingStopsTheServer(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})
	ln := listenOnFreePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var drained atomic.Bool

	err := srv.ServeListener(ctx, ln, time.Second, func() { drained.Store(true) })

	require.NoError(t, err)
	assert.True(t, drained.Load(), "onDrain must still run so readiness reflects the shutdown")
	_, dialErr := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond)
	assert.Error(t, dialErr, "nothing may keep accepting connections after Serve returned")
}

// Cancelling while the listener is still being registered races with startup: Serve must still end with the port closed.
func TestServe_ACancelDuringStartupStillClosesThePort(t *testing.T) {
	for i := range 20 {
		srv := NewServer(ServerConfig{AppName: "fupi-test"})
		ln := listenOnFreePort(t)
		ctx, cancel := context.WithCancel(context.Background())
		served := make(chan error, 1)
		go func() { served <- srv.ServeListener(ctx, ln, time.Second, nil) }()

		time.Sleep(time.Duration(i%5) * 40 * time.Microsecond)
		cancel()

		select {
		case err := <-served:
			require.NoError(t, err, "iteration %d", i)
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Serve did not return", i)
		}
		_, dialErr := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond)
		require.Error(t, dialErr, "iteration %d: the port still accepts connections after Serve returned", i)
	}
}

// verdictAllower is a RateAllower whose verdict the test flips.
type verdictAllower struct{ allowed atomic.Bool }

func (v *verdictAllower) Allow(_ context.Context, _ string, limit redis_rate.Limit) (*redis_rate.Result, error) {
	if v.allowed.Load() {
		return &redis_rate.Result{Limit: limit, Allowed: 1, Remaining: limit.Rate - 1, ResetAfter: time.Minute}, nil
	}
	return &redis_rate.Result{Limit: limit, Allowed: 0, RetryAfter: time.Minute, ResetAfter: time.Minute}, nil
}

// A limited response must not be stored by the idempotency middleware, or the retry with the same key would replay the 429
// after the limit has cleared.
func TestServer_GuardsRunBeforeIdempotency(t *testing.T) {
	verdict := &verdictAllower{}
	verdict.allowed.Store(true)
	limit, err := servermw.RateLimiterMiddleware(servermw.RateLimiterConfig{
		Groups:          []servermw.RateLimitGroup{{Name: "all", Rule: servermw.RateLimitRule{Max: 1, Window: time.Minute}}},
		Redis:           verdict,
		RedisTimeout:    time.Second,
		RedisRetryAfter: time.Second,
	})
	require.NoError(t, err)

	srv := NewServer(ServerConfig{Port: 0, AppName: "fupi-test", Guards: []fiber.Handler{limit}})
	srv.App().Post("/things", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })

	post := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set("X-Idempotency-Key", key)
		resp, err := srv.App().Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}

	const keyA, keyB = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	assert.Equal(t, fiber.StatusCreated, post(keyA))

	verdict.allowed.Store(false)
	assert.Equal(t, fiber.StatusTooManyRequests, post(keyB))

	verdict.allowed.Store(true)
	assert.Equal(t, fiber.StatusCreated, post(keyB), "the 429 must not be replayed for the same idempotency key")
}

// Guards such as authentication run before the handlers, so a panic in one must become a 500, not a crash.
func TestServer_RecoversFromAPanicInAGuard(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "fupi-test", Guards: []fiber.Handler{
		func(*fiber.Ctx) error { panic("guard failed") },
	}})
	srv.App().Get("/things", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	resp, err := srv.App().Test(httptest.NewRequest(http.MethodGet, "/things", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}
