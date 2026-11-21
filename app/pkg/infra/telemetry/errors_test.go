package telemetry

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// captureDefaultLogger sends the default slog logger to the returned buffer until the test ends.
func captureDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestErrorHandler_LogsADistinctErrorOncePerInterval(t *testing.T) {
	buf := captureDefaultLogger(t)
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	handler := newErrorHandler(time.Minute, clock.Now)
	refused := errors.New("rpc error: code = Unavailable desc = connection refused")

	for range 20 {
		handler.Handle(refused)
		clock.now = clock.now.Add(2 * time.Second)
	}
	assert.Equal(t, 1, strings.Count(buf.String(), "opentelemetry error"), "20 repeats within a minute are one line")

	clock.now = clock.now.Add(time.Minute)
	handler.Handle(refused)
	assert.Equal(t, 2, strings.Count(buf.String(), "opentelemetry error"), "the same error is logged again after the interval")
}

func TestErrorHandler_LogsEachDistinctErrorOnItsOwn(t *testing.T) {
	buf := captureDefaultLogger(t)
	handler := newErrorHandler(time.Minute, (&fakeClock{now: time.Unix(1_000, 0)}).Now)

	handler.Handle(errors.New("traces: connection refused"))
	handler.Handle(errors.New("metrics: connection refused"))
	handler.Handle(errors.New("traces: connection refused"))

	assert.Equal(t, 2, strings.Count(buf.String(), "opentelemetry error"))
	assert.Contains(t, buf.String(), "traces: connection refused")
	assert.Contains(t, buf.String(), "metrics: connection refused")
}

func TestErrorHandler_IgnoresANilError(t *testing.T) {
	buf := captureDefaultLogger(t)

	newErrorHandler(time.Minute, time.Now).Handle(nil)

	assert.Empty(t, buf.String())
}

// A flood of ever-different messages must not grow the handler's memory without bound.
func TestErrorHandler_ForgetsOldErrorsWhenTooManyAreDistinct(t *testing.T) {
	captureDefaultLogger(t)
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	handler := newErrorHandler(time.Minute, clock.Now)

	for i := range 10 * maxTrackedErrors {
		handler.Handle(fmt.Errorf("error number %d", i))
	}

	assert.LessOrEqual(t, len(handler.lastSeen), maxTrackedErrors)
}

// A stream of always-different messages passes the per-message check every time, so the line budget has to stop it.
func TestErrorHandler_LogsAtMostAFewLinesPerIntervalWhateverTheMessages(t *testing.T) {
	buf := captureDefaultLogger(t)
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	handler := newErrorHandler(time.Minute, clock.Now)

	for i := range 100 {
		handler.Handle(fmt.Errorf("error number %d", i))
	}
	assert.Equal(t, maxLinesPerInterval, strings.Count(buf.String(), "opentelemetry error"))

	clock.now = clock.now.Add(time.Minute)
	handler.Handle(errors.New("a new error after the interval"))
	assert.Equal(t, maxLinesPerInterval+1, strings.Count(buf.String(), "opentelemetry error"))
}
