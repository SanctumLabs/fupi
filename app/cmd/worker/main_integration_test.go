//go:build integration

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// The whole process against real Postgres and Kafka: it relays a waiting event, answers its probes, and on the stop signal
// finishes, releases the lease and returns without an error.
func TestRun_RelaysAWaitingEventAnswersItsProbesAndStopsCleanly(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OTEL_SDK_DISABLED", "true")

	container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, test.RunDatabaseMigration(ctx, databaseURL))
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)

	id := entity.IDToString(entity.NewID())
	_, err = pool.Exec(ctx, `INSERT INTO outbox_events (id, partition_key, destination, event_type, headers, payload)
		VALUES ($1, 'user-1', 'identity.events', 'user.registered', $2::json, '{"hello":"worker"}'::json)`, id, `{"event_id":"`+id+`"}`)
	require.NoError(t, err)

	port := freePort(t)
	cfg, err := config.LoadWorker(lookupOf(map[string]string{
		"DATABASE_URL": databaseURL, "KAFKA_BROKERS": broker.Brokers[0], "WORKER_HTTP_PORT": port, "SERVER_HOST": "127.0.0.1",
		"OUTBOX_POLL_INTERVAL_MS": "20", "SHUTDOWN_TIMEOUT": "10",
	}))
	require.NoError(t, err)

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	finished := make(chan error, 1)
	go func() { finished <- run(runCtx, cfg) }()

	records := broker.Consume(t, "identity.events", 1, 60*time.Second)
	assert.Equal(t, "user-1", string(records[0].Key))
	assert.JSONEq(t, `{"hello":"worker"}`, string(records[0].Value))
	assert.Equal(t, id, headerOf(records[0].Headers, "event_id"))

	base := fmt.Sprintf("http://127.0.0.1:%s", port)
	status, body := httpGet(t, base+"/health")
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"status":"ok"}`, body)
	require.Eventually(t, func() bool {
		status, body = httpGet(t, base+"/health/ready")
		return status == 200
	}, 15*time.Second, 100*time.Millisecond)
	assert.JSONEq(t, `{"status":"ok","checks":{"postgres":"up","kafka":"up"}}`, body)

	require.Eventually(t, func() bool {
		var sent *time.Time
		_ = pool.QueryRow(ctx, "SELECT sent_time FROM outbox_events WHERE id = $1", id).Scan(&sent)
		return sent != nil
	}, 15*time.Second, 50*time.Millisecond, "the row is recorded as sent")

	stop() // the SIGTERM
	select {
	case err := <-finished:
		assert.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the worker did not stop")
	}
	var holders int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted").Scan(&holders))
	assert.Equal(t, 0, holders, "the lease was released")
}

func headerOf(headers []kgo.RecordHeader, key string) string {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
