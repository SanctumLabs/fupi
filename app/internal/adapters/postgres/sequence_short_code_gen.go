package postgresrepo

import (
	"context"
	"fmt"
	"strings"
	"sync"

	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
)

const (
	// shortCodeBlockSize must equal the INCREMENT BY of short_code_counter (migration 000001): each nextval hands this
	// process the block [value, value+shortCodeBlockSize) and no other process can draw from it.
	shortCodeBlockSize = 1000

	// shortCodeMinLength is the shortest code url.NewShortCode accepts; smaller counter values are left-padded.
	shortCodeMinLength = 6

	base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// sequenceShortCodeGenerator issues short codes from blocks of short_code_counter claimed one database round trip at
// a time, so most calls never touch PostgreSQL. Codes are unique across processes because blocks never overlap; a
// block left unfinished when the process stops is simply skipped.
type sequenceShortCodeGenerator struct {
	claimBlock func(ctx context.Context) (int64, error)
	mu         sync.Mutex
	current    int64
	blockEnd   int64
}

var _ ports.ShortCodeGenerator = (*sequenceShortCodeGenerator)(nil)

// NewSequenceShortCodeGenerator returns a ShortCodeGenerator backed by the short_code_counter sequence.
func NewSequenceShortCodeGenerator(dbClient database.PostgresDatabaseClient) ports.ShortCodeGenerator {
	return &sequenceShortCodeGenerator{
		claimBlock: func(ctx context.Context) (int64, error) {
			return postgresql.New(dbClient.GetDB()).QueryNextShortCodeBlock(ctx)
		},
	}
}

func (g *sequenceShortCodeGenerator) Next(ctx context.Context) (url.ShortCode, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.current >= g.blockEnd {
		blockStart, err := g.claimBlock(ctx)
		if err != nil {
			return url.ShortCode{}, fmt.Errorf("failed to claim a short code block: %w", err)
		}
		g.current = blockStart
		g.blockEnd = blockStart + shortCodeBlockSize
	}

	code := toBase62(g.current)
	g.current++
	return url.NewShortCode(code)
}

// toBase62 encodes n in base62, left-padded with '0' to shortCodeMinLength. Padding keeps codes unique: two different
// values never share a padded encoding.
func toBase62(n int64) string {
	var digits []byte
	for n > 0 {
		digits = append(digits, base62Alphabet[n%62])
		n /= 62
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	if pad := shortCodeMinLength - len(digits); pad > 0 {
		return strings.Repeat("0", pad) + string(digits)
	}
	return string(digits)
}
