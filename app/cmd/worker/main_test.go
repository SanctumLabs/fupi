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

// Postgres is the one required dependency: with it unreachable the process must exit with an error, not hang or relay.
func TestRun_ReturnsAnErrorWhenPostgresIsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about three seconds of connection retries")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true") // no collector to wait for at exit, and none of the developer's to feed
	cfg, err := config.LoadWorker(lookupOf(map[string]string{"DATABASE_PORT": "1", "DATABASE_CONN_TIMEOUT": "1"}))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = run(ctx, cfg)

	require.Error(t, err)
	assert.ErrorContains(t, err, "postgres")
}

func TestRelayConfig_MapsTheSettingsAndKeepsTheRelaysOwnPacing(t *testing.T) {
	cfg := relayConfig(config.OutboxSettings{
		PollInterval: 250 * time.Millisecond, BatchSize: 500, MaxAttempts: 5, StandbyInterval: 2 * time.Second, Retention: 48 * time.Hour,
	})

	assert.Equal(t, 250*time.Millisecond, cfg.PollInterval)
	assert.Equal(t, 500, cfg.BatchSize)
	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 2*time.Second, cfg.StandbyInterval)
	assert.Equal(t, 48*time.Hour, cfg.Retention)
	assert.Equal(t, 5*time.Second, cfg.BacklogInterval)
	assert.Equal(t, 10*time.Minute, cfg.PurgeInterval)
	assert.Equal(t, 1000, cfg.PurgeBatch)
	assert.Equal(t, 200*time.Millisecond, cfg.BackoffMin)
	assert.Equal(t, 10*time.Second, cfg.BackoffMax)
}

// healthServer answers GET /health with status and every other path with 404, like the worker's liveness route.
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
	return lookupOf(map[string]string{"SERVER_HOST": host, "WORKER_HTTP_PORT": port})
}

func TestHealthcheck_ExitsZeroWhenTheWorkerIsAlive(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "ok")
}

func TestHealthcheck_ExitsOneWhenTheLoopIsStalled(t *testing.T) {
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

func TestHealthcheck_NeedsOnlyTheHealthSettings(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	env := map[string]string{"SERVER_HOST": host, "WORKER_HTTP_PORT": port, "KAFKA_BROKERS": "broken", "OUTBOX_BATCH_SIZE": "0"}
	var out bytes.Buffer

	code := healthcheck(lookupOf(env), &out)

	assert.Equal(t, 0, code, "an unrelated bad setting must not make the container unhealthy: %s", out.String())
}

func TestHealthcheck_ReachesAWildcardBindThroughLoopbackAndRejectsABadPort(t *testing.T) {
	_, port := healthServer(t, http.StatusOK)
	for _, host := range []string{"0.0.0.0", "::", ""} {
		var out bytes.Buffer
		assert.Equal(t, 0, healthcheck(probeEnv(host, port), &out), "SERVER_HOST=%q: %s", host, out.String())
	}

	var out bytes.Buffer
	assert.Equal(t, 1, healthcheck(lookupOf(map[string]string{"WORKER_HTTP_PORT": "abc"}), &out))
	assert.Contains(t, out.String(), "WORKER_HTTP_PORT")
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

func TestExpiryConfig_MapsTheSettingsAndTakesTheStandbyIntervalFromTheRelay(t *testing.T) {
	cfg := expiryConfig(config.Worker{
		Expiry: config.ExpirySettings{Interval: 30 * time.Second, BatchSize: 250},
		Outbox: config.OutboxSettings{StandbyInterval: 2 * time.Second},
	})

	assert.Equal(t, 30*time.Second, cfg.Interval)
	assert.Equal(t, 250, cfg.BatchSize)
	assert.Equal(t, 2*time.Second, cfg.StandbyInterval)
}
