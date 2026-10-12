//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run starts a consumer of the topic in the group and returns what it handled so far and a stop function.
func run(t *testing.T, broker *test.KafkaBroker, group string, handler kafka.Handler) (stop func()) {
	t.Helper()
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: broker.Brokers, ClientID: "integration-test", Group: group, Topics: []string{"url.events"},
		RetryMin: 10 * time.Millisecond, RetryMax: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = consumer.Run(ctx, handler) }()
	return func() { cancel(); <-done; consumer.Close() }
}

func TestConsumer_RetriesAFailedRecordAndDoesNotRedeliverCommittedOnes(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "url.events", 1)
	producer := newProducer(t, broker, 10*time.Second)
	errs := producer.Produce(context.Background(), []kafka.Record{
		{Topic: "url.events", Key: []byte("k"), Value: []byte("one"), Headers: []kafka.Header{{Key: "event_id", Value: "e1"}}},
		{Topic: "url.events", Key: []byte("k"), Value: []byte("two"), Headers: []kafka.Header{{Key: "event_id", Value: "e2"}}},
	})
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	var mu sync.Mutex
	var handled []string
	failedOnce := false
	stop := run(t, broker, "scanner", func(_ context.Context, d kafka.Delivery) error {
		mu.Lock()
		defer mu.Unlock()
		if string(d.Value) == "one" && !failedOnce {
			failedOnce = true
			return errors.New("try again")
		}
		handled = append(handled, string(d.Value)+"/"+d.Header("event_id"))
		return nil
	})
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(handled) == 2 }, 60*time.Second, 100*time.Millisecond)
	stop()

	mu.Lock()
	assert.Equal(t, []string{"one/e1", "two/e2"}, handled, "the failed record was retried before the next one")
	handled = nil
	mu.Unlock()

	// A restarted member of the same group resumes after the committed offsets: nothing is handed over again.
	stop = run(t, broker, "scanner", func(_ context.Context, d kafka.Delivery) error {
		mu.Lock()
		defer mu.Unlock()
		handled = append(handled, string(d.Value))
		return nil
	})
	time.Sleep(5 * time.Second)
	stop()
	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, handled)
}
