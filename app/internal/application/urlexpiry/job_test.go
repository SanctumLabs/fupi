package urlexpiry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"
)

var (
	t0       = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	errStore = errors.New("database unavailable")
)

// fakeLeaser scripts the lease: acquireErrs holds the outcome of each Acquire call, later calls succeed.
type fakeLeaser struct {
	acquireErrs []error
	aliveErrs   []error
	acquires    int
	released    int
}

func (l *fakeLeaser) Acquire(context.Context) (ports.Lease, error) {
	l.acquires++
	if len(l.acquireErrs) > 0 {
		err := l.acquireErrs[0]
		l.acquireErrs = l.acquireErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &fakeLease{leaser: l}, nil
}

type fakeLease struct{ leaser *fakeLeaser }

func (l *fakeLease) Alive(context.Context) error {
	if len(l.leaser.aliveErrs) == 0 {
		return nil
	}
	err := l.leaser.aliveErrs[0]
	l.leaser.aliveErrs = l.leaser.aliveErrs[1:]
	return err
}

func (l *fakeLease) Release() { l.leaser.released++ }

type harness struct {
	leaser *fakeLeaser
	urls   *urlmock.MockUrlDatastore
	reader *metric.ManualReader
	job    *Job
	sleeps []time.Duration
	cancel context.CancelFunc
	ctx    context.Context
}

// newHarness builds a job whose clock is fixed at t0 and whose sleep ends the run after stopAfter sleeps.
func newHarness(t *testing.T, stopAfter int, leaser *fakeLeaser) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	h := &harness{leaser: leaser, urls: urlmock.NewMockUrlDatastore(ctrl), reader: metric.NewManualReader()}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	t.Cleanup(h.cancel)

	job, err := NewJob(leaser, h.urls, Config{Interval: time.Minute, StandbyInterval: 5 * time.Second, BatchSize: 2},
		WithMeterProvider(metric.NewMeterProvider(metric.WithReader(h.reader))))
	require.NoError(t, err)
	job.now = func() time.Time { return t0 }
	job.sleep = func(_ context.Context, d time.Duration) {
		h.sleeps = append(h.sleeps, d)
		if len(h.sleeps) >= stopAfter {
			h.cancel()
		}
	}
	h.job = job
	return h
}

func activeUrl(t *testing.T) url.URL {
	t.Helper()
	u, err := urlmock.MockUrl(urlmock.WithId(entity.NewID()), urlmock.WithStatus(url.URLStatusActive),
		urlmock.WithExpiresOn(t0.Add(-time.Hour)))
	require.NoError(t, err)
	return *u
}

// updated records the URLs Update is called with and returns them as the datastore would.
func (h *harness) updated(failFor ...int) *[]url.URL {
	var calls []url.URL
	h.urls.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			calls = append(calls, u)
			for _, n := range failFor {
				if n == len(calls) {
					return url.URL{}, errStore
				}
			}
			return u, nil
		}).AnyTimes()
	return &calls
}

func TestJob_ExpiresTheUrlsFoundAndEmitsUrlExpired(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{})
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t)}, nil)
	calls := h.updated()

	require.NoError(t, h.job.Run(h.ctx))

	require.Len(t, *calls, 1)
	assert.Equal(t, url.URLStatusExpired, (*calls)[0].Status())
	require.Len(t, (*calls)[0].DomainEvents(), 1)
	assert.Equal(t, "url.expired", (*calls)[0].DomainEvents()[0].EventType())
	assert.Equal(t, []time.Duration{time.Minute}, h.sleeps, "then it waits for the next run")
	assert.Equal(t, 1, h.leaser.released)
}

func TestJob_DrainsInBatchesUntilABatchIsShort(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{})
	gomock.InOrder(
		h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t), activeUrl(t)}, nil),
		h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t), activeUrl(t)}, nil),
		h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t)}, nil),
	)
	calls := h.updated()

	require.NoError(t, h.job.Run(h.ctx))

	assert.Len(t, *calls, 5)
}

func TestJob_AFailingUrlDoesNotBlockTheRestOfTheBatch(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{})
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t), activeUrl(t)}, nil)
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t)}, nil)
	calls := h.updated(1)

	require.NoError(t, h.job.Run(h.ctx))

	assert.Len(t, *calls, 3, "the second URL of the batch, and the next batch, are still processed")
}

func TestJob_StopsDrainingWhenAFullBatchMakesNoProgress(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{})
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t), activeUrl(t)}, nil).Times(1)
	calls := h.updated(1, 2)

	require.NoError(t, h.job.Run(h.ctx))

	assert.Len(t, *calls, 2, "the same rows would come back, so the next run retries them")
}

func TestJob_AFetchFailureEndsTheRunAndTheNextOneTriesAgain(t *testing.T) {
	h := newHarness(t, 2, &fakeLeaser{})
	gomock.InOrder(
		h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return(nil, errStore),
		h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t)}, nil),
	)
	calls := h.updated()

	require.NoError(t, h.job.Run(h.ctx))

	assert.Len(t, *calls, 1)
}

func TestJob_ANonLeaderExpiresNothingAndAsksAgain(t *testing.T) {
	h := newHarness(t, 2, &fakeLeaser{acquireErrs: []error{ports.ErrNotLeader, ports.ErrNotLeader}})
	// no datastore call is expected

	require.NoError(t, h.job.Run(h.ctx))

	assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second}, h.sleeps)
	assert.Equal(t, 2, h.leaser.acquires)
}

func TestJob_StopsLeadingWhenTheLeaseIsLostAndAcquiresItAgain(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{aliveErrs: []error{errStore}})
	// the lease is lost before the first run, so no datastore call is expected

	require.NoError(t, h.job.Run(h.ctx))

	assert.Equal(t, 1, h.leaser.released)
	assert.Equal(t, []time.Duration{5 * time.Second}, h.sleeps, "it waits the standby interval before asking again")
}

func TestJob_ReportsTheExpiredCountTheBacklogAndTheLastRun(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{})
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t), activeUrl(t)}, nil)
	h.urls.EXPECT().FetchExpiredActive(gomock.Any(), t0, 2).Return([]url.URL{activeUrl(t)}, nil)
	h.updated(3)
	var seen metricdata.ResourceMetrics
	h.job.sleep = func(ctx context.Context, d time.Duration) { // sample while still the leader
		require.NoError(t, h.reader.Collect(ctx, &seen))
		h.cancel()
	}

	require.NoError(t, h.job.Run(h.ctx))

	assert.Equal(t, int64(2), counterTotal(t, seen, "url.expiry.expired"))
	assert.Equal(t, int64(1), counterTotal(t, seen, "url.expiry.failures"))
	assert.Equal(t, 1.0, gaugeValue(t, seen, "url.expiry.leader"))
	assert.Equal(t, 1.0, gaugeValue(t, seen, "url.expiry.backlog"), "the URL that could not be expired is still waiting")
	assert.Equal(t, float64(t0.Unix()), gaugeValue(t, seen, "url.expiry.last_run"))
}

func TestJob_AStandbyReportsNoBacklogAndNoLastRun(t *testing.T) {
	h := newHarness(t, 1, &fakeLeaser{acquireErrs: []error{ports.ErrNotLeader}})
	var seen metricdata.ResourceMetrics
	h.job.sleep = func(ctx context.Context, d time.Duration) {
		require.NoError(t, h.reader.Collect(ctx, &seen))
		h.cancel()
	}

	require.NoError(t, h.job.Run(h.ctx))

	assert.Equal(t, 0.0, gaugeValue(t, seen, "url.expiry.leader"))
	assert.False(t, hasMetric(seen, "url.expiry.backlog"), "a zero would read as an empty backlog")
	assert.False(t, hasMetric(seen, "url.expiry.last_run"))
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

func counterTotal(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "%s is %T", name, m.Data)
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

func gaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			require.True(t, ok, "%s is %T", name, m.Data)
			require.Len(t, gauge.DataPoints, 1)
			return gauge.DataPoints[0].Value
		}
	}
	require.Failf(t, "metric not reported", "%s", name)
	return 0
}
