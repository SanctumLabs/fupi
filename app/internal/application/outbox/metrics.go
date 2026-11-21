package outbox

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/internal/application/outbox"

// metrics are the relay's instruments. The backlog numbers are sampled by the leader every few seconds and read by the
// gauges' callbacks; the oldest event's age is computed when it is read, so it keeps growing between samples.
type metrics struct {
	published metric.Int64Counter
	failures  metric.Int64Counter
	parked    metric.Int64Counter
	duration  metric.Float64Histogram

	leader       atomic.Bool
	unsent       atomic.Int64
	parkedRows   atomic.Int64
	oldestUnsent atomic.Int64 // unix nanoseconds; 0 when nothing waits
}

func newMetrics(provider metric.MeterProvider, now func() time.Time) (*metrics, error) {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	meter := provider.Meter(instrumentationName)
	m := &metrics{}

	var err error
	counter := func(name, description string) metric.Int64Counter {
		c, counterErr := meter.Int64Counter(name, metric.WithDescription(description))
		if counterErr != nil && err == nil {
			err = counterErr
		}
		return c
	}
	m.published = counter("outbox.relay.published", "Events acknowledged by the broker.")
	m.failures = counter("outbox.relay.failures", "Events the broker did not acknowledge, by kind (transient or permanent).")
	m.parked = counter("outbox.relay.parked", "Events the relay gave up on.")

	if err != nil {
		return nil, err
	}

	var histogramErr error
	if m.duration, histogramErr = meter.Float64Histogram("outbox.relay.publish.duration",
		metric.WithUnit("s"), metric.WithDescription("Time to publish one batch.")); histogramErr != nil {
		return nil, histogramErr
	}

	// leaderOnly gauges describe the outbox, which only the active relay samples. A standby reports no data point at all:
	// a zero would read as an empty backlog and keep the backlog alerts silent while nobody delivers.
	gauge := func(name, unit, description string, leaderOnly bool, read func() float64) {
		_, gaugeErr := meter.Float64ObservableGauge(name, metric.WithUnit(unit), metric.WithDescription(description),
			metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
				if leaderOnly && !m.leader.Load() {
					return nil
				}
				observer.Observe(read())
				return nil
			}))
		if gaugeErr != nil && err == nil {
			err = gaugeErr
		}
	}
	gauge("outbox.relay.leader", "{instance}", "1 while this instance is the active relay.", false, func() float64 {
		if m.leader.Load() {
			return 1
		}
		return 0
	})
	gauge("outbox.relay.backlog", "{event}", "Events waiting for delivery.", true, func() float64 { return float64(m.unsent.Load()) })
	gauge("outbox.relay.parked_rows", "{event}", "Events the relay gave up on.", true, func() float64 { return float64(m.parkedRows.Load()) })
	gauge("outbox.relay.oldest_unsent_age", "s", "Age of the oldest event waiting for delivery.", true, func() float64 {
		oldest := m.oldestUnsent.Load()
		if oldest == 0 {
			return 0
		}
		return now().Sub(time.Unix(0, oldest)).Seconds()
	})
	return m, err
}
