//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
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
