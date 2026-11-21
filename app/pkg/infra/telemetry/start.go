package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// flushTimeout bounds the final export of spans and metrics at shutdown.
const flushTimeout = 5 * time.Second

// setup is Setup. It is a variable so a test can replace it.
var setup = Setup

// Start installs the OpenTelemetry SDK and returns the function that flushes it. Call the flush once the server or the
// relay has stopped, so the last spans and the final metrics are exported; it flushes only once, however often it is
// called. Telemetry never stops the process: when the SDK cannot start the process runs without it, and a failing flush is
// only logged.
func Start(ctx context.Context, opts Options) (flush func()) {
	shutdown, err := setup(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "telemetry is disabled: the OpenTelemetry SDK could not start", "error", err)
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// ctx is already cancelled when this runs (that is what began the shutdown), so the flush gets its own deadline.
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
			defer cancel()
			if err := shutdown(flushCtx); err != nil {
				slog.WarnContext(flushCtx, "flushing telemetry", "error", err)
			}
		})
	}
}
