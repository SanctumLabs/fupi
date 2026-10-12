package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	defaultRetryMin = time.Second
	defaultRetryMax = 30 * time.Second
	// commitTimeout bounds the commit of a record that was handled, which must still go through while the process shuts down.
	commitTimeout = 5 * time.Second
)

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Brokers are the seed brokers, as host:port.
	Brokers []string
	// ClientID names this client to the brokers.
	ClientID string
	// Group is the consumer group; its members share the partitions of the topics and its committed offsets say where to resume.
	Group string
	// Topics are the topics to consume.
	Topics []string
	// RetryMin and RetryMax bound the wait, doubling from the first, between attempts to handle a record that failed. Zero
	// means one second and thirty seconds.
	RetryMin, RetryMax time.Duration
}

// Delivery is a record handed to a Handler.
type Delivery struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   []Header
}

// Header returns the value of the first header with the key, or "" if there is none.
func (d Delivery) Header(key string) string {
	for _, header := range d.Headers {
		if header.Key == key {
			return header.Value
		}
	}
	return ""
}

// Handler handles one record. An error means the record could not be handled yet: it is not committed and is handed over
// again after a wait. A record that can never be handled must be reported as success (after logging it), or it blocks its
// partition for good.
type Handler func(ctx context.Context, delivery Delivery) error

// client is the part of kgo.Client the consumer uses.
type client interface {
	PollFetches(ctx context.Context) kgo.Fetches
	CommitRecords(ctx context.Context, records ...*kgo.Record) error
	AllowRebalance()
	Ping(ctx context.Context) error
	Close()
}

// Consumer consumes topics as a member of a consumer group with at-least-once delivery: an offset is committed only after
// the handler returned success for its record, and records are handled one at a time in the order of the log, so a record
// that keeps failing holds back the ones after it. A handler must be idempotent: a record can be handed over again after
// a crash, a failed commit or a rebalance. Like the Producer it connects lazily.
type Consumer struct {
	client   client
	retryMin time.Duration
	retryMax time.Duration
}

// NewConsumer builds a Consumer. extra options are applied last and are for tests. A new group starts at the beginning of
// the topics.
func NewConsumer(cfg ConsumerConfig, extra ...kgo.Opt) (*Consumer, error) {
	switch {
	case len(cfg.Brokers) == 0:
		return nil, errors.New("kafka: at least one broker is required")
	case cfg.Group == "":
		return nil, errors.New("kafka: a consumer group is required")
	case len(cfg.Topics) == 0:
		return nil, errors.New("kafka: at least one topic is required")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumerGroup(cfg.Group),
		kgo.ConsumeTopics(cfg.Topics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// Offsets are committed by hand, after the handler succeeded.
		kgo.DisableAutoCommit(),
		// A rebalance waits until the records of a poll were handled and committed, instead of taking the partitions away mid-way.
		kgo.BlockRebalanceOnPoll(),
	}
	kafkaClient, err := kgo.NewClient(append(opts, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("create the kafka client: %w", err)
	}
	consumer := &Consumer{client: kafkaClient, retryMin: cfg.RetryMin, retryMax: cfg.RetryMax}
	if consumer.retryMin <= 0 {
		consumer.retryMin = defaultRetryMin
	}
	if consumer.retryMax <= 0 {
		consumer.retryMax = defaultRetryMax
	}
	return consumer, nil
}

// Run hands the records to the handler until ctx is cancelled, which is not an error, or the client is closed.
func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		for _, fetchErr := range fetches.Errors() {
			if errors.Is(fetchErr.Err, kgo.ErrClientClosed) {
				return fetchErr.Err
			}
			slog.WarnContext(ctx, "kafka fetch failed", "topic", fetchErr.Topic, "partition", fetchErr.Partition, "error", fetchErr.Err)
		}

		if fetches.NumRecords() == 0 {
			if len(fetches.Errors()) > 0 && !sleep(ctx, c.retryMin) {
				return nil
			}
			continue
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			if !c.deliver(ctx, iter.Next(), handler) {
				return nil
			}
		}
		c.client.AllowRebalance()
	}
}

// deliver hands the record to the handler until it succeeds, then commits it. It returns false if ctx ended first, in
// which case the record is left uncommitted.
func (c *Consumer) deliver(ctx context.Context, record *kgo.Record, handler Handler) bool {
	delivery := toDelivery(record)
	wait := c.retryMin
	for {
		err := handler(ctx, delivery)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return false
		}
		slog.WarnContext(ctx, "kafka record not handled, retrying", "topic", delivery.Topic, "partition", delivery.Partition, "offset", delivery.Offset, "retry_in", wait.String(), "error", err)
		if !sleep(ctx, wait) {
			return false
		}
		wait = min(wait*2, c.retryMax)
	}

	// The record was handled: commit it even if the process is shutting down. A commit that fails is only logged, since the
	// record will be handed over again and handlers are idempotent.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancel()
	if err := c.client.CommitRecords(commitCtx, record); err != nil {
		slog.WarnContext(ctx, "kafka offset not committed, the record may be delivered again", "topic", delivery.Topic, "partition", delivery.Partition, "offset", delivery.Offset, "error", err)
	}
	return true
}

// Ping reports whether any broker answers.
func (c *Consumer) Ping(ctx context.Context) error { return c.client.Ping(ctx) }

// Close leaves the group and closes the client.
func (c *Consumer) Close() { c.client.Close() }

func toDelivery(record *kgo.Record) Delivery {
	headers := make([]Header, len(record.Headers))
	for i, header := range record.Headers {
		headers[i] = Header{Key: header.Key, Value: string(header.Value)}
	}
	return Delivery{
		Topic: record.Topic, Partition: record.Partition, Offset: record.Offset,
		Key: record.Key, Value: record.Value, Headers: headers,
	}
}

// sleep waits for d and reports false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
