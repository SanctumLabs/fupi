package health

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
)

// DefaultCheckTimeout bounds each dependency check, so a hung dependency cannot hang the readiness endpoint.
const DefaultCheckTimeout = 2 * time.Second

// Status is the overall readiness verdict.
type Status string

const (
	// StatusOK means every check is up.
	StatusOK Status = "ok"
	// StatusDegraded means only optional checks are down; the process can still serve traffic.
	StatusDegraded Status = "degraded"
	// StatusUnavailable means a required check is down.
	StatusUnavailable Status = "unavailable"
	// StatusDraining means the process is shutting down and wants no new traffic.
	StatusDraining Status = "draining"
)

// Check probes one dependency. A Required check that fails makes the process not ready; an optional one only
// degrades it.
type Check struct {
	Name     string
	Required bool
	Fn       func(ctx context.Context) error
}

// Report is the readiness result. Checks maps a check name to "up" or "down". It never carries error text: the
// endpoint is public, and failures are logged server-side instead.
type Report struct {
	Status Status            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Ready reports whether traffic should be sent to the process.
func (r Report) Ready() bool { return r.Status == StatusOK || r.Status == StatusDegraded }

// Registry runs the registered checks. Add every check before serving; Add is not safe for concurrent use.
type Registry struct {
	timeout  time.Duration
	checks   []Check
	draining atomic.Bool
}

// NewRegistry creates a registry whose checks each get at most timeout.
func NewRegistry(timeout time.Duration) *Registry {
	return &Registry{timeout: timeout}
}

// Add registers a check.
func (r *Registry) Add(check Check) {
	r.checks = append(r.checks, check)
}

// SetDraining flips readiness to "draining" for the rest of the process's life. Call it when shutdown begins.
func (r *Registry) SetDraining() {
	slog.Info("readiness: draining, no longer accepting new traffic")
	r.draining.Store(true)
}

// Run executes every check in parallel, each bounded by the registry's timeout. The checks run under an unsampled parent
// span, so the Postgres and Redis pings they make leave no spans: the probes are polled every few seconds and would
// otherwise start a trace each time.
func (r *Registry) Run(ctx context.Context) Report {
	if r.draining.Load() {
		return Report{Status: StatusDraining, Checks: map[string]string{}}
	}

	checkCtx := telemetry.Unsampled(ctx)
	errs := make([]error, len(r.checks))
	var wg sync.WaitGroup
	for i, check := range r.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.runOne(checkCtx, check)
		}()
	}
	wg.Wait()

	report := Report{Status: StatusOK, Checks: make(map[string]string, len(r.checks))}
	for i, check := range r.checks {
		if errs[i] == nil {
			report.Checks[check.Name] = "up"
			continue
		}
		report.Checks[check.Name] = "down"
		slog.WarnContext(ctx, "health check failed", "check", check.Name, "required", check.Required, "error", errs[i])
		if check.Required {
			report.Status = StatusUnavailable
		} else if report.Status == StatusOK {
			report.Status = StatusDegraded
		}
	}
	return report
}

// runOne runs a check in its own goroutine and gives up after the timeout even if the check ignores its context. A
// check that never returns leaks its goroutine; that is the price of a bounded readiness endpoint.
func (r *Registry) runOne(ctx context.Context, check Check) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- check.Fn(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
