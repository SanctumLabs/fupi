package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// fakeClient serves scripted polls and records the commits.
type fakeClient struct {
	mu        sync.Mutex
	polls     []kgo.Fetches
	committed []int64
	commitErr error
	allowed   int
	closed    bool
}

func (f *fakeClient) PollFetches(ctx context.Context) kgo.Fetches {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.polls) == 0 {
		f.mu.Unlock()
		<-ctx.Done()
		f.mu.Lock()
		return kgo.NewErrFetch(ctx.Err())
	}
	next := f.polls[0]
	f.polls = f.polls[1:]
	return next
}

func (f *fakeClient) CommitRecords(_ context.Context, records ...*kgo.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range records {
		f.committed = append(f.committed, r.Offset)
	}
	return f.commitErr
}

func (f *fakeClient) AllowRebalance()            { f.mu.Lock(); f.allowed++; f.mu.Unlock() }
func (f *fakeClient) Ping(context.Context) error { return nil }
func (f *fakeClient) Close()                     { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

func (f *fakeClient) commits() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.committed...)
}

func fetchOf(records ...*kgo.Record) kgo.Fetches {
	return kgo.Fetches{{Topics: []kgo.FetchTopic{{Topic: "url.events", Partitions: []kgo.FetchPartition{{Partition: 0, Records: records}}}}}}
}

func record(offset int64, value string, headers ...kgo.RecordHeader) *kgo.Record {
	return &kgo.Record{Topic: "url.events", Partition: 0, Offset: offset, Key: []byte("k"), Value: []byte(value), Headers: headers}
}

func newTestConsumer(client *fakeClient) *Consumer {
	return &Consumer{client: client, retryMin: time.Millisecond, retryMax: 5 * time.Millisecond}
}

func TestRun_CommitsEachRecordOnlyAfterItWasHandled(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{fetchOf(record(1, "a"), record(2, "b")), fetchOf(record(3, "c"))}}
	ctx, cancel := context.WithCancel(context.Background())
	var handled []string
	var commitsAtHandling [][]int64

	err := newTestConsumer(client).Run(ctx, func(_ context.Context, d Delivery) error {
		handled = append(handled, string(d.Value))
		commitsAtHandling = append(commitsAtHandling, client.commits())
		if len(handled) == 3 {
			cancel()
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, handled)
	assert.Equal(t, [][]int64{nil, {1}, {1, 2}}, commitsAtHandling, "a record is committed after its handler returned, not before")
	assert.Equal(t, []int64{1, 2, 3}, client.commits(), "the last record is committed even though the context was cancelled while handling it")
	assert.Equal(t, 2, client.allowed, "rebalances are allowed again after each poll's records")
}

func TestRun_ARecordThatFailsIsRetriedUntilItSucceedsAndOnlyThenCommitted(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{fetchOf(record(1, "a"), record(2, "b"))}}
	ctx, cancel := context.WithCancel(context.Background())
	attempts := map[string]int{}

	err := newTestConsumer(client).Run(ctx, func(_ context.Context, d Delivery) error {
		attempts[string(d.Value)]++
		if string(d.Value) == "a" && attempts["a"] < 3 {
			assert.Empty(t, client.commits())
			return errors.New("the scanner is down")
		}
		if string(d.Value) == "b" {
			cancel()
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, attempts["a"])
	assert.Equal(t, []int64{1, 2}, client.commits(), "order is kept: b waited for a")
}

func TestRun_StopsWithoutCommittingARecordItCouldNotHandle(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{fetchOf(record(1, "a"))}}
	ctx, cancel := context.WithCancel(context.Background())

	err := newTestConsumer(client).Run(ctx, func(_ context.Context, _ Delivery) error {
		cancel()
		return errors.New("the scanner is down")
	})

	require.NoError(t, err)
	assert.Empty(t, client.commits(), "a failed record stays uncommitted so it is delivered again")
}

func TestRun_ACommitFailureDoesNotStopTheConsumer(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{fetchOf(record(1, "a"), record(2, "b"))}, commitErr: errors.New("rebalance in progress")}
	ctx, cancel := context.WithCancel(context.Background())
	count := 0

	err := newTestConsumer(client).Run(ctx, func(_ context.Context, _ Delivery) error {
		if count++; count == 2 {
			cancel()
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRun_FetchErrorsAreSurvived(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{kgo.NewErrFetch(errors.New("broker down")), fetchOf(record(1, "a"))}}
	ctx, cancel := context.WithCancel(context.Background())

	err := newTestConsumer(client).Run(ctx, func(_ context.Context, _ Delivery) error {
		cancel()
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{1}, client.commits())
}

func TestRun_ReturnsWhenTheClientIsClosed(t *testing.T) {
	client := &fakeClient{polls: []kgo.Fetches{kgo.NewErrFetch(kgo.ErrClientClosed)}}

	err := newTestConsumer(client).Run(context.Background(), func(context.Context, Delivery) error { return nil })

	require.ErrorIs(t, err, kgo.ErrClientClosed)
}

func TestToDelivery_CarriesTheRecord(t *testing.T) {
	d := toDelivery(record(7, "payload", kgo.RecordHeader{Key: "event_id", Value: []byte("e-1")}))

	assert.Equal(t, Delivery{
		Topic: "url.events", Partition: 0, Offset: 7, Key: []byte("k"), Value: []byte("payload"),
		Headers: []Header{{Key: "event_id", Value: "e-1"}},
	}, d)
	assert.Equal(t, "e-1", d.Header("event_id"))
	assert.Equal(t, "", d.Header("missing"))
}

func TestNewConsumer_NeedsBrokersAGroupAndTopicsButNeverDialsOne(t *testing.T) {
	for name, cfg := range map[string]ConsumerConfig{
		"no brokers": {Group: "g", Topics: []string{"t"}},
		"no group":   {Brokers: []string{"127.0.0.1:1"}, Topics: []string{"t"}},
		"no topics":  {Brokers: []string{"127.0.0.1:1"}, Group: "g"},
	} {
		_, err := NewConsumer(cfg)
		assert.Error(t, err, name)
	}

	consumer, err := NewConsumer(ConsumerConfig{Brokers: []string{"127.0.0.1:1"}, ClientID: "test", Group: "g", Topics: []string{"t"}})
	require.NoError(t, err, "a broker that is down must not fail construction")
	consumer.Close()
}
