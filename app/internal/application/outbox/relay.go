// Package outbox is the transactional outbox relay (ADR-0011): one leader-elected worker that drains outbox_events into
// the broker with at-least-once delivery, keeps one key's events in order, parks events the broker permanently rejects
// and purges old sent rows.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// maxReasonLength bounds the error text stored on a row.
const maxReasonLength = 500

// Config tunes the relay. Every field must be set; NewConfig-style defaults live in the worker's configuration.
type Config struct {
	// PollInterval is the wait when a cycle finds nothing to deliver.
	PollInterval time.Duration
	// BatchSize is how many events per destination one cycle claims.
	BatchSize int
	// MaxAttempts is how many permanent rejections park an event.
	MaxAttempts int
	// StandbyInterval is how often an instance that is not the leader tries to become it.
	StandbyInterval time.Duration
	// Retention is how long sent events are kept; zero turns the purge off.
	Retention time.Duration
	// BacklogInterval is how often the leader samples the backlog for the gauges.
	BacklogInterval time.Duration
	// PurgeInterval is how often the leader purges; PurgeBatch is how many rows one purge statement deletes.
	PurgeInterval time.Duration
	PurgeBatch    int
	// BackoffMin and BackoffMax bound the wait after a cycle that delivered nothing; it doubles up to the maximum.
	BackoffMin time.Duration
	BackoffMax time.Duration
}

// Relay delivers the outbox to a broker.
type Relay struct {
	store     ports.OutboxDatastore
	publisher ports.EventPublisher
	cfg       Config

	tracer  trace.Tracer
	metrics *metrics

	// now and sleep are replaced by tests.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration)

	lastBeat atomic.Int64 // unix nanoseconds of the last completed cycle or standby attempt
}

// Option customises a Relay.
type Option func(*options)

type options struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// WithTracerProvider sets the tracer provider; the default is the global one.
func WithTracerProvider(provider trace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = provider }
}

// WithMeterProvider sets the meter provider; the default is the global one.
func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(o *options) { o.meterProvider = provider }
}

// NewRelay builds a relay. It returns an error only if the metric instruments cannot be created.
func NewRelay(store ports.OutboxDatastore, publisher ports.EventPublisher, cfg Config, opts ...Option) (*Relay, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.tracerProvider == nil {
		o.tracerProvider = otel.GetTracerProvider()
	}

	r := &Relay{
		store:     store,
		publisher: publisher,
		cfg:       cfg,
		tracer:    o.tracerProvider.Tracer(instrumentationName),
		now:       time.Now,
		sleep:     sleepContext,
	}
	var err error
	if r.metrics, err = newMetrics(o.meterProvider, func() time.Time { return r.now() }); err != nil {
		return nil, fmt.Errorf("create the outbox relay metrics: %w", err)
	}
	r.beat()
	return r, nil
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Healthy reports whether the relay has completed a cycle, or a standby attempt, within maxAge. A wedged loop is not.
func (r *Relay) Healthy(maxAge time.Duration) bool {
	return r.now().Sub(time.Unix(0, r.lastBeat.Load())) <= maxAge
}

func (r *Relay) beat() { r.lastBeat.Store(r.now().UnixNano()) }

// Run relays until ctx is cancelled and returns nil. An instance that is not the leader waits and retries; the leader
// delivers until it loses its lease or ctx ends. The batch in flight when ctx ends is finished first.
func (r *Relay) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		lease, err := r.store.Acquire(telemetry.Unsampled(ctx))
		if err != nil {
			if !errors.Is(err, ports.ErrNotLeader) {
				slog.WarnContext(ctx, "outbox relay: cannot take the lease", "error", err)
			}
			r.beat()
			r.sleep(ctx, r.cfg.StandbyInterval)
			continue
		}
		r.lead(ctx, lease)
		lease.Release()
		if ctx.Err() != nil {
			break
		}
		// Wait before asking again: a connection that keeps dying right after it is opened must not become a hot loop.
		r.sleep(ctx, r.cfg.StandbyInterval)
	}
	return nil
}

// lead delivers while it holds the lease.
func (r *Relay) lead(ctx context.Context, lease ports.Lease) {
	slog.InfoContext(ctx, "outbox relay: this instance is the leader")
	r.metrics.leader.Store(true)
	defer func() {
		// A standby has no view of the backlog: clear what it sampled so a stale number cannot outlive the leadership.
		r.metrics.leader.Store(false)
		r.metrics.unsent.Store(0)
		r.metrics.parkedRows.Store(0)
		r.metrics.oldestUnsent.Store(0)
		slog.InfoContext(ctx, "outbox relay: this instance is no longer the leader")
	}()

	var lastBacklog, lastPurge time.Time
	backoff := r.cfg.BackoffMin
	for ctx.Err() == nil {
		r.beat()
		housekeeping := telemetry.Unsampled(ctx)

		if err := lease.Alive(housekeeping); err != nil {
			slog.WarnContext(ctx, "outbox relay: lost the lease", "error", err)
			return
		}
		if now := r.now(); now.Sub(lastBacklog) >= r.cfg.BacklogInterval {
			r.sampleBacklog(housekeeping)
			lastBacklog = now
		}
		if now := r.now(); r.cfg.Retention > 0 && now.Sub(lastPurge) >= r.cfg.PurgeInterval {
			r.purge(housekeeping)
			lastPurge = now
		}

		events, err := r.store.Claim(housekeeping, r.cfg.BatchSize)
		if err != nil {
			slog.WarnContext(ctx, "outbox relay: cannot claim events", "error", err)
			r.sleep(ctx, backoff)
			backoff = r.nextBackoff(backoff)
			continue
		}
		if len(events) == 0 {
			r.sleep(ctx, r.cfg.PollInterval)
			continue
		}

		if r.deliver(ctx, events) {
			backoff = r.cfg.BackoffMin
			continue
		}
		r.sleep(ctx, backoff)
		backoff = r.nextBackoff(backoff)
	}
}

func (r *Relay) nextBackoff(current time.Duration) time.Duration {
	return min(current*2, r.cfg.BackoffMax)
}

// deliver publishes the claimed events and records the outcome of each. It reports whether at least one event was
// acknowledged and recorded as sent. The publish and the mark-as-sent run on a context that survives the shutdown
// signal, so a batch that is in flight when the process is told to stop is finished and recorded, not left to be
// published twice.
func (r *Relay) deliver(ctx context.Context, events []ports.OutboxEvent) bool {
	work := context.WithoutCancel(ctx)

	spans := make([]trace.Span, len(events))
	messages := make([]ports.Message, len(events))
	for i, event := range events {
		spanCtx, span := r.tracer.Start(storedTraceContext(work, event), event.Destination+" publish",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.String("messaging.destination.name", event.Destination),
				attribute.String("messaging.operation.type", "publish"),
				attribute.String("messaging.message.id", event.ID),
				attribute.String("outbox.event_type", event.EventType),
			))
		spans[i] = span
		messages[i] = buildMessage(spanCtx, event)
	}

	start := r.now()
	results := r.publisher.Publish(telemetry.Unsampled(work), messages)
	r.metrics.duration.Record(work, r.now().Sub(start).Seconds())

	if len(results) != len(events) {
		slog.ErrorContext(ctx, "outbox relay: the publisher returned the wrong number of results",
			"events", len(events), "results", len(results))
		results = make([]ports.PublishResult, len(events))
		for i := range results {
			results[i] = ports.PublishResult{Err: errors.New("no result from the publisher")}
		}
	}

	var sent []string
	for i, result := range results {
		event, span := events[i], spans[i]
		destination := attribute.String("destination", event.Destination)
		switch {
		case result.Err == nil:
			sent = append(sent, event.ID)
			r.metrics.published.Add(work, 1, metric.WithAttributes(destination))
		case result.Permanent:
			r.metrics.failures.Add(work, 1, metric.WithAttributes(attribute.String("kind", "permanent")))
			span.SetStatus(codes.Error, "permanent")
			r.reject(work, event, result.Err)
		default:
			r.metrics.failures.Add(work, 1, metric.WithAttributes(attribute.String("kind", "transient")))
			span.SetStatus(codes.Error, "transient")
			slog.WarnContext(ctx, "outbox relay: publish failed, will retry",
				"event_id", event.ID, "destination", event.Destination, "error", result.Err)
		}
		span.End()
	}

	if len(sent) == 0 {
		return false
	}
	if err := r.store.MarkSent(telemetry.Unsampled(work), sent); err != nil {
		slog.ErrorContext(ctx, "outbox relay: published events could not be marked as sent; they will be published again",
			"events", len(sent), "error", err)
		return false
	}
	return true
}

// reject counts a permanent rejection and parks the event once it has used up its attempts.
func (r *Relay) reject(ctx context.Context, event ports.OutboxEvent, cause error) {
	housekeeping := telemetry.Unsampled(ctx)
	reason := truncate(cause.Error(), maxReasonLength)

	attempts, err := r.store.RecordRejection(housekeeping, event.ID, reason)
	if err != nil {
		slog.ErrorContext(ctx, "outbox relay: cannot record a rejection", "event_id", event.ID, "error", err)
		return
	}
	slog.WarnContext(ctx, "outbox relay: the broker rejected an event",
		"event_id", event.ID, "destination", event.Destination, "attempt", attempts, "max_attempts", r.cfg.MaxAttempts, "error", cause)
	if attempts < r.cfg.MaxAttempts {
		return
	}
	if err := r.store.Park(housekeeping, event.ID, reason); err != nil {
		slog.ErrorContext(ctx, "outbox relay: cannot park an event", "event_id", event.ID, "error", err)
		return
	}
	r.metrics.parked.Add(ctx, 1)
	slog.ErrorContext(ctx, "outbox relay: parked an event the broker keeps rejecting; fix the cause, then re-queue it",
		"event_id", event.ID, "destination", event.Destination, "event_type", event.EventType, "reason", reason)
}

func (r *Relay) sampleBacklog(ctx context.Context) {
	backlog, err := r.store.Backlog(ctx)
	if err != nil {
		slog.WarnContext(ctx, "outbox relay: cannot read the backlog", "error", err)
		return
	}
	r.metrics.unsent.Store(backlog.Unsent)
	r.metrics.parkedRows.Store(backlog.Parked)
	if backlog.OldestUnsent.IsZero() {
		r.metrics.oldestUnsent.Store(0)
		return
	}
	r.metrics.oldestUnsent.Store(backlog.OldestUnsent.UnixNano())
}

// purge deletes sent events older than the retention, in batches, until a batch is not full.
func (r *Relay) purge(ctx context.Context) {
	cutoff := r.now().Add(-r.cfg.Retention)
	var total int
	for ctx.Err() == nil {
		deleted, err := r.store.Purge(ctx, cutoff, r.cfg.PurgeBatch)
		if err != nil {
			slog.WarnContext(ctx, "outbox relay: cannot purge sent events", "error", err)
			break
		}
		total += deleted
		if deleted < r.cfg.PurgeBatch {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "outbox relay: purged sent events", "deleted", total, "older_than", cutoff)
	}
}

// truncate cuts s to at most limit bytes without splitting a character: the result is stored in a text column, which rejects
// invalid UTF-8.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
