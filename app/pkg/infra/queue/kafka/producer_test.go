package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIsPermanent(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                             {nil, false},
		"message too large":               {kerr.MessageTooLarge, true},
		"record list too large":           {kerr.RecordListTooLarge, true},
		"invalid topic":                   {kerr.InvalidTopicException, true},
		"invalid record":                  {kerr.InvalidRecord, true},
		"unsupported for the format":      {kerr.UnsupportedForMessageFormat, true},
		"topic authorization is config":   {kerr.TopicAuthorizationFailed, false},
		"cluster authorization is config": {kerr.ClusterAuthorizationFailed, false},
		"wrapped permanent":               {fmt.Errorf("produce: %w", kerr.MessageTooLarge), true},
		"unknown topic may be created":    {kerr.UnknownTopicOrPartition, false},
		"not leader":                      {kerr.NotLeaderForPartition, false},
		"request timed out":               {kerr.RequestTimedOut, false},
		"unknown server error":            {kerr.UnknownServerError, false},
		"record timeout":                  {kgo.ErrRecordTimeout, false},
		"context deadline":                {context.DeadlineExceeded, false},
		"client closed":                   {kgo.ErrClientClosed, false},
		"plain error":                     {errors.New("connection refused"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPermanent(tc.err))
		})
	}
}

func TestNewProducer_NeedsABrokerButNeverDialsOne(t *testing.T) {
	_, err := NewProducer(Config{ClientID: "test", PublishTimeout: time.Second})
	assert.Error(t, err)

	start := time.Now()
	producer, err := NewProducer(Config{Brokers: []string{"127.0.0.1:1"}, ClientID: "test", PublishTimeout: time.Second})
	require.NoError(t, err, "a broker that is down must not fail construction")
	t.Cleanup(producer.Close)
	assert.Less(t, time.Since(start), time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, producer.Ping(ctx), "Ping reports the broker as unreachable")
}

func TestProduce_FailsEveryRecordWithATransientErrorWhenNoBrokerAnswers(t *testing.T) {
	producer, err := NewProducer(Config{Brokers: []string{"127.0.0.1:1"}, ClientID: "test", PublishTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	start := time.Now()
	errs := producer.Produce(context.Background(), []Record{
		{Topic: "t", Key: []byte("k"), Value: []byte("1")},
		{Topic: "t", Key: []byte("k"), Value: []byte("2")},
	})

	require.Len(t, errs, 2)
	for i, err := range errs {
		require.Error(t, err, "record %d", i)
		assert.False(t, IsPermanent(err), "a broker that is down is not the record's fault: %v", err)
	}
	assert.Less(t, time.Since(start), 15*time.Second, "bounded by the delivery timeout")
}
