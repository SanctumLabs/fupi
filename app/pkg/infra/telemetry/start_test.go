package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setupFunc = func(context.Context, Options) (func(context.Context) error, error)

func stubSetup(t *testing.T, stub setupFunc) {
	t.Helper()
	previous := setup
	setup = stub
	t.Cleanup(func() { setup = previous })
}

func TestStart_PassesTheOptionsOn(t *testing.T) {
	var got Options
	stubSetup(t, func(_ context.Context, opts Options) (func(context.Context) error, error) {
		got = opts
		return func(context.Context) error { return nil }, nil
	})

	Start(context.Background(), Options{ServiceName: "fupi-worker", ServiceVersion: "1.2.3", Environment: "staging"})()

	assert.Equal(t, Options{ServiceName: "fupi-worker", ServiceVersion: "1.2.3", Environment: "staging"}, got)
}

// The run context is cancelled by the SIGTERM that starts the shutdown, so a flush that reused it would give up at once.
func TestStart_TheFlushGetsItsOwnDeadlineAfterTheRunContextIsCancelled(t *testing.T) {
	var flushErr error
	var hasDeadline bool
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(ctx context.Context) error {
			flushErr = ctx.Err()
			_, hasDeadline = ctx.Deadline()
			return nil
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	flush := Start(ctx, Options{})
	cancel() // the SIGTERM

	flush()

	require.NoError(t, flushErr, "the flush context must not be the cancelled run context")
	assert.True(t, hasDeadline, "and it must be bounded")
}

// A telemetry problem must never stop the process.
func TestStart_ASetupFailureDisablesTelemetryInsteadOfStopping(t *testing.T) {
	buf := captureDefaultLogger(t)
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return nil, errors.New("bad exporter configuration")
	})

	flush := Start(context.Background(), Options{})

	require.NotNil(t, flush)
	assert.NotPanics(t, flush)
	assert.Contains(t, buf.String(), "telemetry is disabled")
	assert.Contains(t, buf.String(), "bad exporter configuration")
}

func TestStart_AFailingFlushIsLoggedNotFatal(t *testing.T) {
	buf := captureDefaultLogger(t)
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(context.Context) error { return errors.New("collector unreachable") }, nil
	})

	assert.NotPanics(t, Start(context.Background(), Options{}))

	assert.Contains(t, buf.String(), "flushing telemetry")
	assert.Contains(t, buf.String(), "collector unreachable")
}

// run flushes right after the server drains and again from a defer that covers its early returns.
func TestStart_TheFlushRunsOnlyOnce(t *testing.T) {
	var shutdowns int
	stubSetup(t, func(context.Context, Options) (func(context.Context) error, error) {
		return func(context.Context) error {
			shutdowns++
			return nil
		}, nil
	})

	flush := Start(context.Background(), Options{})
	flush()
	flush()

	assert.Equal(t, 1, shutdowns)
}
