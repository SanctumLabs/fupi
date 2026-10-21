package telemetry

import (
	"log/slog"
	"sync"
	"time"
)

// maxTrackedErrors bounds the memory of the handler: when more distinct errors than this are seen within one interval the
// oldest records are forgotten, which at worst repeats a line.
const maxTrackedErrors = 64

// maxLinesPerInterval bounds the lines the handler writes per interval whatever the messages are, so a stream of
// always-different errors cannot flood the log either.
const maxLinesPerInterval = 10

// errorHandler is the OpenTelemetry error handler. The SDK reports every failed export to it, which for an unreachable
// collector means one error every few seconds for as long as the collector is away. It logs each distinct error at
// most once per interval, and at most maxLinesPerInterval lines per interval in all, so the log carries one line a minute
// instead.
type errorHandler struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time
	lastSeen map[string]time.Time

	windowStart time.Time
	windowLines int
}

func newErrorHandler(interval time.Duration, now func() time.Time) *errorHandler {
	return &errorHandler{interval: interval, now: now, lastSeen: map[string]time.Time{}}
}

// Handle implements otel.ErrorHandler.
func (h *errorHandler) Handle(err error) {
	if err == nil {
		return
	}
	if !h.admit(err.Error()) {
		return
	}
	slog.Warn("opentelemetry error", "error", err)
}

// admit reports whether message has not been logged within the interval and the interval's line budget is not spent, and
// records that it is being logged now.
func (h *errorHandler) admit(message string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	if last, seen := h.lastSeen[message]; seen && now.Sub(last) < h.interval {
		return false
	}
	if now.Sub(h.windowStart) >= h.interval {
		h.windowStart, h.windowLines = now, 0
	}
	if h.windowLines >= maxLinesPerInterval {
		return false
	}
	h.windowLines++
	if len(h.lastSeen) >= maxTrackedErrors {
		for key, last := range h.lastSeen {
			if now.Sub(last) >= h.interval {
				delete(h.lastSeen, key)
			}
		}
		if len(h.lastSeen) >= maxTrackedErrors {
			clear(h.lastSeen)
		}
	}
	h.lastSeen[message] = now
	return true
}
