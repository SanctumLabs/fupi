//go:build integration

package kafka_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func newProducer(t *testing.T, broker *test.KafkaBroker, timeout time.Duration, extra ...kgo.Opt) *kafka.Producer {
	t.Helper()
	producer, err := kafka.NewProducer(kafka.Config{Brokers: broker.Brokers, ClientID: "integration-test", PublishTimeout: timeout}, extra...)
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	return producer
}

func headerValue(record *kgo.Record, key string) (string, bool) {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}

func TestProducer_DeliversKeysValuesAndHeadersAndKeepsEachKeysOrder(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	var records []kafka.Record
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("user-%d", i%3)
		records = append(records, kafka.Record{
			Topic: "identity.events", Key: []byte(key), Value: []byte(fmt.Sprintf(`{"key":"%s","n":%d}`, key, i)),
			Headers: []kafka.Header{{Key: "event_id", Value: fmt.Sprintf("e%d", i)}, {Key: "traceparent", Value: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}},
		})
	}

	errs := producer.Produce(context.Background(), records)
	for i, err := range errs {
		require.NoError(t, err, "record %d", i)
	}
	require.NoError(t, producer.Ping(context.Background()))

	consumed := broker.Consume(t, "identity.events", 12, 30*time.Second)
	partitionOf := map[string]int32{}
	nextExpected := map[string]int{"user-0": 0, "user-1": 1, "user-2": 2}
	for _, record := range consumed {
		key := string(record.Key)
		if known, ok := partitionOf[key]; ok {
			assert.Equal(t, known, record.Partition, "one key stays on one partition")
		}
		partitionOf[key] = record.Partition

		assert.Contains(t, string(record.Value), fmt.Sprintf(`"n":%d`, nextExpected[key]), "key %s arrived out of order", key)
		nextExpected[key] += 3

		eventID, ok := headerValue(record, "event_id")
		require.True(t, ok)
		assert.True(t, strings.HasPrefix(eventID, "e"))
		traceparent, ok := headerValue(record, "traceparent")
		require.True(t, ok)
		assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", traceparent)
	}
	assert.Len(t, partitionOf, 3)
}

func TestProducer_ARecordWithoutAKeyIsDelivered(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "url.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	errs := producer.Produce(context.Background(), []kafka.Record{{Topic: "url.events", Value: []byte(`{}`)}})

	require.NoError(t, errs[0])
	consumed := broker.Consume(t, "url.events", 1, 30*time.Second)
	assert.Nil(t, consumed[0].Key)
}

func TestProducer_AnOversizedRecordIsPermanentWhileTheOthersInTheBatchArrive(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 10*time.Second)

	errs := producer.Produce(context.Background(), []kafka.Record{
		{Topic: "identity.events", Key: []byte("a"), Value: []byte("small-1")},
		{Topic: "identity.events", Key: []byte("b"), Value: make([]byte, 2_000_000)},
		{Topic: "identity.events", Key: []byte("c"), Value: []byte("small-2")},
	})

	require.Len(t, errs, 3)
	assert.NoError(t, errs[0])
	require.Error(t, errs[1])
	assert.True(t, kafka.IsPermanent(errs[1]), "too large will never succeed: %v", errs[1])
	assert.NoError(t, errs[2])
	assert.Len(t, broker.Consume(t, "identity.events", 2, 30*time.Second), 2)
}

// With topic auto-creation off, a missing topic may be created later, so it is transient: the relay waits, it does not park.
func TestProducer_AMissingTopicIsTransient(t *testing.T) {
	broker := test.StartKafka(t)
	producer := newProducer(t, broker, 3*time.Second, kgo.UnknownTopicRetries(1))

	start := time.Now()
	errs := producer.Produce(context.Background(), []kafka.Record{{Topic: "no.such.topic", Key: []byte("k"), Value: []byte("v")}})

	require.Error(t, errs[0])
	assert.False(t, kafka.IsPermanent(errs[0]), "%v", errs[0])
	assert.Less(t, time.Since(start), 30*time.Second)
}

// The outage drill in miniature: a batch fails as a whole while the broker is away (the partition stays gapless), the
// same batch succeeds after the broker is back, and a reader of the topic sees every record once, in order.
func TestProducer_ABatchThatFailsDuringAnOutageIsDeliveredInOrderAfterwards(t *testing.T) {
	broker := test.StartKafka(t)
	broker.CreateTopic(t, "identity.events", 3)
	producer := newProducer(t, broker, 3*time.Second)
	batch := func(from, to int) []kafka.Record {
		var records []kafka.Record
		for n := from; n <= to; n++ {
			records = append(records, kafka.Record{Topic: "identity.events", Key: []byte("user-1"), Value: []byte(fmt.Sprintf("%02d", n))})
		}
		return records
	}

	for i, err := range producer.Produce(context.Background(), batch(1, 5)) {
		require.NoError(t, err, "record %d", i+1)
	}

	broker.Stop(t)
	errs := producer.Produce(context.Background(), batch(6, 10))
	for i, err := range errs {
		require.Error(t, err, "record %d must fail while the broker is away", i+6)
		assert.False(t, kafka.IsPermanent(err), "%v", err)
	}
	assert.Error(t, producer.Ping(context.Background()), "the broker is down")

	broker.Start(t)
	for i, err := range producer.Produce(context.Background(), batch(6, 10)) {
		require.NoError(t, err, "record %d after the restart", i+6)
	}

	var seen []string
	for _, record := range broker.Consume(t, "identity.events", 10, 60*time.Second) {
		seen = append(seen, string(record.Value))
	}
	assert.Equal(t, []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10"}, seen, "every record once, in order")
}
