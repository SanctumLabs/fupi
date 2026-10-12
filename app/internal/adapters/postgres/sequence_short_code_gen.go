package postgresrepo

import (
	"context"
	"errors"
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

	// shortCodeLength is the length of every generated code. Codes issued before ADR-0019 are 6 characters, so the two
	// sets cannot collide. url.NewShortCode accepts 6 to 10.
	shortCodeLength = 7

	// shortCodeSpace is the number of shortCodeLength-character base62 codes, 62^7: the permutation's domain and the
	// counter value at which the generator runs out (ADR-0019 says what happens then).
	shortCodeSpace = 3_521_614_606_208

	base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// errShortCodeSpaceExhausted means short_code_counter has passed shortCodeSpace: every 7-character code is issued.
var errShortCodeSpaceExhausted = errors.New("the short code space is exhausted")

// sequenceShortCodeGenerator issues short codes from blocks of short_code_counter claimed one database round trip at
// a time, so most calls never touch PostgreSQL. Codes are unique across processes because blocks never overlap; a
// block left unfinished when the process stops is simply skipped.
//
// The counter is not exposed: each value goes through a keyed permutation before it is encoded, so codes reveal
// neither their order nor how many links exist, and the next code cannot be guessed from the last (ADR-0019).
type sequenceShortCodeGenerator struct {
	claimBlock  func(ctx context.Context) (int64, error)
	permutation keyedPermutation
	mu          sync.Mutex
	current     int64
	blockEnd    int64
}

var _ ports.ShortCodeGenerator = (*sequenceShortCodeGenerator)(nil)

// NewSequenceShortCodeGenerator returns a ShortCodeGenerator backed by the short_code_counter sequence and keyed by key.
// The key must never change once codes have been issued with it: a different key maps later counter values onto
// codes that may already exist.
func NewSequenceShortCodeGenerator(dbClient database.PostgresDatabaseClient, key []byte) ports.ShortCodeGenerator {
	return &sequenceShortCodeGenerator{
		claimBlock: func(ctx context.Context) (int64, error) {
			return postgresql.New(dbClient.GetDB()).QueryNextShortCodeBlock(ctx)
		},
		permutation: newKeyedPermutation(key, shortCodeSpace),
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

	if g.current < 0 || g.current >= shortCodeSpace {
		return url.ShortCode{}, errShortCodeSpaceExhausted
	}

	code := toBase62(g.permutation.apply(uint64(g.current)))
	g.current++
	return url.NewShortCode(code)
}

// toBase62 encodes n in base62, left-padded with '0' to shortCodeLength. Padding keeps codes unique: two different
// values never share a padded encoding.
func toBase62(n uint64) string {
	var digits []byte
	for n > 0 {
		digits = append(digits, base62Alphabet[n%62])
		n /= 62
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	if pad := shortCodeLength - len(digits); pad > 0 {
		return strings.Repeat("0", pad) + string(digits)
	}
	return string(digits)
}
