package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Config configures a Producer.
type Config struct {
	// Brokers are the seed brokers, as host:port.
	Brokers []string
	// ClientID names this client to the brokers.
	ClientID string
	// PublishTimeout is how long a record may wait to be acknowledged before it fails with a timeout. Once one record of
	// a partition times out the records buffered after it for that partition fail too, which keeps the partition gapless.
	PublishTimeout time.Duration
}

// Header is a record header.
type Header struct {
	Key   string
	Value string
}

// Record is a message to produce.
type Record struct {
	Topic string
	// Key decides the partition: records with the same key go to the same partition in the order they are given. Nil
	// lets the client spread the record.
	Key     []byte
	Value   []byte
	Headers []Header
}

// Producer produces records to Kafka with an idempotent, all-replicas-acknowledged producer. It connects lazily:
// building one never fails because the brokers are down. Every Produce call is bounded by the publish timeout.
//
// By default franz-go refuses to fail a record whose request is already on its way, because with idempotent writes it
// cannot know whether the broker stored it; a broker that dies mid-request then keeps Produce waiting until it comes
// back, deadline or not. The outbox relay needs a bounded call and tolerates duplicates (it delivers at least once and
// consumers de-duplicate on the event ID), so cancellation of in-flight records is allowed.
type Producer struct {
	client  *kgo.Client
	timeout time.Duration
}

// NewProducer builds a Producer. extra options are applied last and are for tests.
func NewProducer(cfg Config, extra ...kgo.Opt) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: at least one broker is required")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		// Idempotent writes are the franz-go default and need all in-sync replicas to acknowledge; saying so keeps it explicit.
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordDeliveryTimeout(cfg.PublishTimeout),
		kgo.AllowIdempotentProduceCancellation(),
	}
	client, err := kgo.NewClient(append(opts, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("create the kafka client: %w", err)
	}
	return &Producer{client: client, timeout: cfg.PublishTimeout}, nil
}

// Produce produces the records and waits until each one is acknowledged or has failed, at most for the publish timeout. It returns one error per record,
// in the order of records; nil means the broker acknowledged it. Records with the same key reach their partition in the
// order given, and if one fails the ones after it for that partition fail too.
func (p *Producer) Produce(ctx context.Context, records []Record) []error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	pending := make([]*kgo.Record, len(records))
	index := make(map[*kgo.Record]int, len(records))
	for i, record := range records {
		headers := make([]kgo.RecordHeader, len(record.Headers))
		for j, header := range record.Headers {
			headers[j] = kgo.RecordHeader{Key: header.Key, Value: []byte(header.Value)}
		}
		pending[i] = &kgo.Record{Topic: record.Topic, Key: record.Key, Value: record.Value, Headers: headers}
		index[pending[i]] = i
	}

	// ProduceSync returns the results in the order the broker answered, not the order of the records, so each result is
	// matched to its record by the record it carries.
	errs := make([]error, len(records))
	for _, result := range p.client.ProduceSync(ctx, pending...) {
		errs[index[result.Record]] = result.Err
	}
	return errs
}

// Ping reports whether any broker answers.
func (p *Producer) Ping(ctx context.Context) error { return p.client.Ping(ctx) }

// Close closes the client.
func (p *Producer) Close() { p.client.Close() }

// permanentErrors are the broker answers that will never change for the same record: it is too large, its topic name is
// invalid, or it is malformed. Everything else, including the broker being down, a timeout, an unknown topic
// (auto-creation is off, so it may be created later) and every retriable answer, is transient. The two authorization
// errors are transient on purpose: they say the client's configuration is wrong, not the record, and a mistaken ACL
// would otherwise park the whole backlog within seconds; as transient errors the events wait and flow once it is fixed.
var permanentErrors = []*kerr.Error{
	kerr.MessageTooLarge,
	kerr.RecordListTooLarge,
	kerr.InvalidTopicException,
	kerr.InvalidRecord,
	kerr.UnsupportedForMessageFormat,
}

// IsPermanent reports whether err says the broker will never accept the record as it is.
func IsPermanent(err error) bool {
	for _, permanent := range permanentErrors {
		if errors.Is(err, permanent) {
			return true
		}
	}
	return false
}
