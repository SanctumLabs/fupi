package ports

import (
	"context"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
)

// RedirectLookup is what a RedirectCache knows about a code.
type RedirectLookup int

const (
	// RedirectUnknown means the cache holds nothing for the code: ask the datastore.
	RedirectUnknown RedirectLookup = iota
	// RedirectFound means the cache holds the code's target.
	RedirectFound
	// RedirectAbsent means the cache remembers that the datastore has no such code.
	RedirectAbsent
)

// RedirectCache remembers where short codes and custom aliases redirect to, so the hot path can skip the datastore.
//
// It is an optimization and never a source of truth (ADR-0015): every method may fail, and callers carry on without it.
// It stores targets, not decisions; the caller decides on every hit with RedirectTarget.Redirect.
type RedirectCache interface {
	// Get returns the cached target of the code. The target is only meaningful when the lookup is RedirectFound.
	Get(ctx context.Context, code string) (url.RedirectTarget, RedirectLookup, error)
	// Put remembers the target of the code for ttl.
	Put(ctx context.Context, code string, target url.RedirectTarget, ttl time.Duration) error
	// PutAbsent remembers for ttl that no url has the code.
	PutAbsent(ctx context.Context, code string, ttl time.Duration) error
	// Evict forgets the codes, found and absent alike.
	Evict(ctx context.Context, codes ...string) error
}
