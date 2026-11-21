// Package urlexpiry is the URL expiry job: one leader-elected worker that moves ACTIVE URLs past their expiry to EXPIRED,
// so a URL's status is accurate and url.expired reaches the outbox (ADR-0002, ADR-0011). Redirects refuse an expired URL
// on their own; the job does not make them correct, it makes the status and the event follow.
package urlexpiry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/internal/application/urlexpiry"

// Config tunes the job. Every field must be set.
type Config struct {
	// Interval is the wait between runs.
	Interval time.Duration
	// StandbyInterval is how often an instance that is not the leader tries to become it.
	StandbyInterval time.Duration
	// BatchSize is how many URLs one fetch loads.
	BatchSize int
}

// Job expires URLs.
type Job struct {
	leaser ports.Leaser
	urls   url.UrlDatastore
	cfg    Config

	metrics *metrics

	// now and sleep are replaced by tests.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration)
}

// Option customises a Job.
type Option func(*options)

type options struct {
	meterProvider metric.MeterProvider
}

// WithMeterProvider sets the meter provider; the default is the global one.
func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(o *options) { o.meterProvider = provider }
}

// NewJob builds a job. It returns an error only if the metric instruments cannot be created.
func NewJob(leaser ports.Leaser, urls url.UrlDatastore, cfg Config, opts ...Option) (*Job, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.meterProvider == nil {
		o.meterProvider = otel.GetMeterProvider()
	}
	m, err := newMetrics(o.meterProvider)
	if err != nil {
		return nil, fmt.Errorf("create the url expiry metrics: %w", err)
	}
	return &Job{leaser: leaser, urls: urls, cfg: cfg, metrics: m, now: time.Now, sleep: sleepContext}, nil
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Run expires URLs until ctx is cancelled and returns nil. An instance that is not the leader waits and retries; the
// leader runs every Interval until it loses its lease or ctx ends.
func (j *Job) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		lease, err := j.leaser.Acquire(telemetry.Unsampled(ctx))
		if err != nil {
			if !errors.Is(err, ports.ErrNotLeader) {
				slog.WarnContext(ctx, "url expiry: cannot take the lease", "error", err)
			}
			j.sleep(ctx, j.cfg.StandbyInterval)
			continue
		}
		j.lead(ctx, lease)
		lease.Release()
		if ctx.Err() != nil {
			break
		}
		// Wait before asking again: a connection that keeps dying right after it is opened must not become a hot loop.
		j.sleep(ctx, j.cfg.StandbyInterval)
	}
	return nil
}

// lead runs while it holds the lease.
func (j *Job) lead(ctx context.Context, lease ports.Lease) {
	slog.InfoContext(ctx, "url expiry: this instance is the leader")
	j.metrics.leader.Store(true)
	defer func() {
		// A standby has no view of the backlog: clear what it sampled so a stale number cannot outlive the leadership.
		j.metrics.leader.Store(false)
		j.metrics.backlog.Store(0)
		j.metrics.lastRun.Store(0)
		slog.InfoContext(ctx, "url expiry: this instance is no longer the leader")
	}()

	for ctx.Err() == nil {
		if err := lease.Alive(telemetry.Unsampled(ctx)); err != nil {
			slog.WarnContext(ctx, "url expiry: lost the lease", "error", err)
			return
		}
		j.drain(ctx)
		j.sleep(ctx, j.cfg.Interval)
	}
}

// drain expires the URLs that are past their expiry as of now, a batch at a time, then measures what is left. A run
// stops when a batch is short (the backlog is empty), when a full batch made no progress (the same rows would come back;
// the next run retries them) or when a fetch fails.
func (j *Job) drain(ctx context.Context) {
	before := j.now()
	j.expireBatches(ctx, before)
	if ctx.Err() == nil {
		j.sampleBacklog(ctx, before)
	}
}

func (j *Job) expireBatches(ctx context.Context, before time.Time) {
	for ctx.Err() == nil {
		batch, err := j.urls.FetchExpiredActive(telemetry.Unsampled(ctx), before, j.cfg.BatchSize)
		if err != nil {
			slog.WarnContext(ctx, "url expiry: cannot fetch the expired urls", "error", err)
			return
		}
		handled := 0
		for _, u := range batch {
			if ctx.Err() != nil {
				break
			}
			if j.expire(ctx, u) {
				handled++
			}
		}
		j.metrics.lastRun.Store(before.Unix())
		if len(batch) < j.cfg.BatchSize || handled == 0 {
			return
		}
	}
}

// sampleBacklog counts the URLs still past their expiry, however many batches that is.
func (j *Job) sampleBacklog(ctx context.Context, before time.Time) {
	backlog, err := j.urls.CountExpiredActive(telemetry.Unsampled(ctx), before)
	if err != nil {
		slog.WarnContext(ctx, "url expiry: cannot count the expired urls", "error", err)
		return
	}
	j.metrics.backlog.Store(backlog)
}

// expire moves one URL to EXPIRED. Update writes the status and url.expired to the outbox in one transaction, so a URL
// that fails is skipped and the others are not held back. It reports whether the URL needs no retry: it was expired, or
// another writer changed it since it was loaded (a Conflict: it was suspended or its expiry moved), in which case the
// job leaves it alone and a later run sees its new state.
func (j *Job) expire(ctx context.Context, u url.URL) bool {
	if err := u.MarkExpired(); err != nil {
		slog.WarnContext(ctx, "url expiry: cannot expire a url", "url_id", u.ID(), "error", err)
		j.metrics.failures.Add(ctx, 1)
		return false
	}
	if _, err := j.urls.Update(ctx, u); err != nil {
		if errdefs.IsConflict(err) {
			slog.InfoContext(ctx, "url expiry: a url changed since it was loaded, leaving it alone", "url_id", u.ID())
			return true
		}
		slog.WarnContext(ctx, "url expiry: cannot save an expired url", "url_id", u.ID(), "error", err)
		j.metrics.failures.Add(ctx, 1)
		return false
	}
	j.metrics.expired.Add(ctx, 1)
	return true
}
