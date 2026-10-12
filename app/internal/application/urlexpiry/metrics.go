package urlexpiry

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"
)

// metrics are the job's instruments. The backlog and last-run gauges describe the leader's last run, so a standby
// reports no data point at all: a zero would read as an empty backlog and keep the alerts silent while nobody expires.
type metrics struct {
	expired  metric.Int64Counter
	failures metric.Int64Counter

	leader  atomic.Bool
	backlog atomic.Int64 // URLs still ACTIVE past their expiry at the end of the last run
	lastRun atomic.Int64 // unix seconds of the last run that fetched; 0 before the first
}

func newMetrics(provider metric.MeterProvider) (*metrics, error) {
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
	m.expired = counter("url.expiry.expired", "URLs moved to EXPIRED.")
	m.failures = counter("url.expiry.failures", "URLs the job could not move to EXPIRED.")

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
	gauge("url.expiry.leader", "{instance}", "1 while this instance is the active expiry job.", false, func() float64 {
		if m.leader.Load() {
			return 1
		}
		return 0
	})
	gauge("url.expiry.backlog", "{url}", "URLs past their expiry that were still ACTIVE at the end of the last run.", true,
		func() float64 { return float64(m.backlog.Load()) })
	gauge("url.expiry.last_run", "s", "Unix time of the last run.", true,
		func() float64 { return float64(m.lastRun.Load()) })
	return m, err
}
