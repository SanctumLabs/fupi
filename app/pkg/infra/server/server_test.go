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

	"github.com/gofiber/fiber/v2"
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
