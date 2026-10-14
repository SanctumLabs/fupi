//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// With Postgres up and Redis down the API serves, reports itself degraded, and stops cleanly when its context ends.
func TestRun_ServesReadinessAndStopsCleanlyOnCancel(t *testing.T) {
	stubTelemetry(t)
	ctx := context.Background()
	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))

	port := freePort(t)
	cfg, err := config.Load(lookupOf(map[string]string{
		"DATABASE_URL":  connectionString,
		"HTTP_PORT":     strconv.Itoa(port),
		"REDIS_ADDRESS": "127.0.0.1:1",
	}))
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(runCtx, cfg) }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health/ready")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 200*time.Millisecond, "the API never became ready")

	resp, err := http.Get(base + "/health/ready")
	require.NoError(t, err)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	assert.Equal(t, "degraded", body.Status)
	assert.Equal(t, "up", body.Checks["postgres"])
	assert.Equal(t, "down", body.Checks["redis"])

	live, err := http.Get(base + "/health")
	require.NoError(t, err)
	live.Body.Close()
	assert.Equal(t, http.StatusOK, live.StatusCode)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}
}

// syncBuffer is a bytes.Buffer safe for the server's goroutines to log into while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var verificationLinkPattern = regexp.MustCompile(`https?://[^\s"]+/auth/verify\?v=[0-9a-f]+`)

// With no email transport, a developer verifies an account by following the link in the API's log. The link must be
// there, and it must reach the verify route the API actually serves.
func TestRun_LogsAVerificationLinkThatVerifies(t *testing.T) {
	ctx := context.Background()
	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))

	logs := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cfg, err := config.Load(lookupOf(map[string]string{
		"ENVIRONMENT":   "development",
		"DATABASE_URL":  connectionString,
		"HTTP_PORT":     strconv.Itoa(port),
		"APP_BASE_URL":  base,
		"REDIS_ADDRESS": "127.0.0.1:1",
	}))
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(runCtx, cfg) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 200*time.Millisecond, "the API never came up")

	registration := `{"username":"janedoe","first_name":"Jane","email":"janedoe@example.com","password":"Pas%word134"}`
	resp, err := http.Post(base+baseURI+"/auth/register", "application/json", strings.NewReader(registration))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	link := verificationLinkPattern.FindString(logs.String())
	require.NotEmpty(t, link, "no verification link in the log:\n%s", logs.String())
	assert.True(t, strings.HasPrefix(link, base+baseURI+"/auth/verify?v="), "the link %q must point at the API's verify route", link)

	verified, err := http.Get(link)
	require.NoError(t, err)
	var user struct {
		Verified bool `json:"verified"`
	}
	require.NoError(t, json.NewDecoder(verified.Body).Decode(&user))
	verified.Body.Close()
	assert.Equal(t, http.StatusOK, verified.StatusCode)
	assert.True(t, user.Verified)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}
}
