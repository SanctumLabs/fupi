package postgresrepo

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
)

// PostgreSQL sequence-backed implementation
type sequenceShortCodeGenerator struct {
	pool      *pgxpool.Pool
	mu        sync.Mutex
	current   int64
	rangeEnd  int64
	rangeSize int64
}

func (g *sequenceShortCodeGenerator) Next(ctx context.Context) (url.ShortCode, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current >= g.rangeEnd {
		// if err := g.claimNextRange(ctx); err != nil {
		return url.ShortCode{}, nil
	}

	// code := toBase62(g.current)
	// g.current++
	return url.ShortCode{}, nil
	// return url.NewShortCode(code)
}
