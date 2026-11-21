package kafkaadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
)

type fakeProducer struct {
	produce func(records []kafka.Record) []error
	records []kafka.Record
	pinged  bool
	closed  bool
}

func (f *fakeProducer) Produce(_ context.Context, records []kafka.Record) []error {
	f.records = records
	return f.produce(records)
}
func (f *fakeProducer) Ping(context.Context) error { f.pinged = true; return nil }
func (f *fakeProducer) Close()                     { f.closed = true }

func TestPublish_MapsMessagesOntoRecords(t *testing.T) {
	fake := &fakeProducer{produce: func(r []kafka.Record) []error { return make([]error, len(r)) }}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), []ports.Message{{
		Topic: "identity.events", Key: []byte("user-1"), Value: []byte(`{"a":1}`),
		Headers: []ports.Header{{Key: "event_id", Value: "e1"}, {Key: "traceparent", Value: "00-aa-bb-01"}},
	}, {Topic: "url.events", Value: []byte(`{}`)}})

	require.Len(t, fake.records, 2)
	assert.Equal(t, kafka.Record{
		Topic: "identity.events", Key: []byte("user-1"), Value: []byte(`{"a":1}`),
		Headers: []kafka.Header{{Key: "event_id", Value: "e1"}, {Key: "traceparent", Value: "00-aa-bb-01"}},
	}, fake.records[0])
	assert.Nil(t, fake.records[1].Key)
	assert.Equal(t, []ports.PublishResult{{}, {}}, results)
}

func TestPublish_ClassifiesEachFailureOnItsOwn(t *testing.T) {
	transient := errors.New("broker not available")
	fake := &fakeProducer{produce: func([]kafka.Record) []error {
		return []error{nil, kerr.MessageTooLarge, transient, kerr.UnknownTopicOrPartition}
	}}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), make([]ports.Message, 4))

	require.Len(t, results, 4)
	assert.Equal(t, ports.PublishResult{}, results[0])
	assert.Equal(t, ports.PublishResult{Err: kerr.MessageTooLarge, Permanent: true}, results[1])
	assert.Equal(t, ports.PublishResult{Err: transient}, results[2])
	assert.False(t, results[3].Permanent, "an unknown topic may be created later")
	assert.Error(t, results[3].Err)
}

func TestPublish_AMissingResultIsATransientFailure(t *testing.T) {
	fake := &fakeProducer{produce: func([]kafka.Record) []error { return []error{nil} }}
	publisher := &EventPublisher{producer: fake}

	results := publisher.Publish(context.Background(), make([]ports.Message, 2))

	assert.NoError(t, results[0].Err)
	assert.Error(t, results[1].Err)
	assert.False(t, results[1].Permanent)
}

func TestPingAndCloseReachTheProducer(t *testing.T) {
	fake := &fakeProducer{}
	publisher := &EventPublisher{producer: fake}

	assert.NoError(t, publisher.Ping(context.Background()))
	publisher.Close()

	assert.True(t, fake.pinged)
	assert.True(t, fake.closed)
}
