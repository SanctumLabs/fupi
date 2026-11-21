package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
)

// fakeStore is an in-memory ports.OutboxDatastore. Its behaviour is scripted by the fields a test sets.
type fakeStore struct {
	acquireErrs []error // one per Acquire call; later calls succeed
	aliveErrs   []error // one per Alive call across all leases; later calls succeed
	claims      [][]ports.OutboxEvent
	claimErr    error
	markErr     error
	purgeCounts []int // rows each Purge call reports deleting; later calls delete nothing
	backlog     ports.Backlog

	acquires   int
	claimCalls int
	released   int
	markSent   [][]string
	attempts   map[string]int
	rejections []string
	parked     []string
	purges     []purgeCall
	housekeep  []context.Context // contexts the relay used for its housekeeping queries
}

type purgeCall struct {
	olderThan time.Time
	limit     int
}

func (s *fakeStore) Acquire(ctx context.Context) (ports.Lease, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.acquires++
	if len(s.acquireErrs) > 0 {
		err := s.acquireErrs[0]
		s.acquireErrs = s.acquireErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &fakeLease{store: s}, nil
}

func (s *fakeStore) Claim(ctx context.Context, perDestination int) ([]ports.OutboxEvent, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.claimCalls++
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if len(s.claims) == 0 {
		return nil, nil
	}
	batch := s.claims[0]
	s.claims = s.claims[1:]
	return batch, nil
}

func (s *fakeStore) MarkSent(ctx context.Context, ids []string) error {
	s.housekeep = append(s.housekeep, ctx)
	if s.markErr != nil {
		return s.markErr
	}
	s.markSent = append(s.markSent, append([]string(nil), ids...))
	return nil
}

func (s *fakeStore) RecordRejection(ctx context.Context, id, reason string) (int, error) {
	s.housekeep = append(s.housekeep, ctx)
	if s.attempts == nil {
		s.attempts = map[string]int{}
	}
	s.attempts[id]++
	s.rejections = append(s.rejections, id)
	return s.attempts[id], nil
}

func (s *fakeStore) Park(ctx context.Context, id, reason string) error {
	s.housekeep = append(s.housekeep, ctx)
	s.parked = append(s.parked, id)
	return nil
}

func (s *fakeStore) Purge(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	s.housekeep = append(s.housekeep, ctx)
	s.purges = append(s.purges, purgeCall{olderThan: olderThan, limit: limit})
	if len(s.purgeCounts) == 0 {
		return 0, nil
	}
	deleted := s.purgeCounts[0]
	s.purgeCounts = s.purgeCounts[1:]
	return deleted, nil
}

func (s *fakeStore) Backlog(ctx context.Context) (ports.Backlog, error) {
	s.housekeep = append(s.housekeep, ctx)
	return s.backlog, nil
}

type fakeLease struct{ store *fakeStore }

func (l *fakeLease) Alive(ctx context.Context) error {
	l.store.housekeep = append(l.store.housekeep, ctx)
	if len(l.store.aliveErrs) == 0 {
		return nil
	}
	err := l.store.aliveErrs[0]
	l.store.aliveErrs = l.store.aliveErrs[1:]
	return err
}

func (l *fakeLease) Release() { l.store.released++ }

// fakePublisher is a scripted ports.EventPublisher.
type fakePublisher struct {
	// publish decides the results; nil acknowledges everything.
	publish func(call int, messages []ports.Message) []ports.PublishResult

	calls    [][]ports.Message
	contexts []context.Context
}

func (p *fakePublisher) Publish(ctx context.Context, messages []ports.Message) []ports.PublishResult {
	p.contexts = append(p.contexts, ctx)
	p.calls = append(p.calls, append([]ports.Message(nil), messages...))
	if p.publish != nil {
		return p.publish(len(p.calls), messages)
	}
	return acked(len(messages))
}

func (p *fakePublisher) Ping(context.Context) error { return nil }
func (p *fakePublisher) Close()                     {}

func acked(n int) []ports.PublishResult { return make([]ports.PublishResult, n) }

var (
	errTransient = errors.New("broker not available")
	errPermanent = errors.New("message too large")
)

func transient() ports.PublishResult { return ports.PublishResult{Err: errTransient} }
func permanent() ports.PublishResult { return ports.PublishResult{Err: errPermanent, Permanent: true} }
