package postgres

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client used golang.org/x/exp/slog, a separate package with its own default logger, so its lines bypassed the
// process logger: they were plain text in a JSON stream and never carried a trace ID.
func TestNewPostgresClient_LogsThroughTheDefaultSlogLogger(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about three seconds of connection retries")
	}
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := NewPostgresClient(PostgresDatabaseConfig{
		Host: "127.0.0.1", Port: "1", Name: "fupidb", Username: "u", Password: "p", SslMode: "disable",
		MaxConns: 1, ConnTimeout: time.Second,
	})

	require.Error(t, err)
	assert.Contains(t, buf.String(), "connecting to database")
	assert.Contains(t, buf.String(), `"level":"ERROR"`)
}
