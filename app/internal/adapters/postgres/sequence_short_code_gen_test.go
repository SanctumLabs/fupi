package postgresrepo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testShortCodeKey = []byte("a-test-key-that-is-at-least-32-bytes-long")

func newTestGenerator(claimBlock func(context.Context) (int64, error)) *sequenceShortCodeGenerator {
	return &sequenceShortCodeGenerator{claimBlock: claimBlock, permutation: newKeyedPermutation(testShortCodeKey, shortCodeSpace)}
}

func TestToBase62(t *testing.T) {
	cases := map[uint64]string{
		0:                  "0000000",
		1:                  "0000001",
		61:                 "000000Z",
		62:                 "0000010",
		shortCodeSpace - 1: "ZZZZZZZ",
	}

	for n, want := range cases {
		assert.Equal(t, want, toBase62(n), "toBase62(%d)", n)
	}
}

// Every value in a small domain must map to a distinct value in the same domain: the permutation is a bijection, which
// is what keeps generated codes unique. The domains cover an exact power of two, an odd bit count and the cycle walk.
func TestKeyedPermutation_IsABijectionOnItsDomain(t *testing.T) {
	for _, domain := range []uint64{2, 62, 1000, 1024, 3844, 20_000} {
		permutation := newKeyedPermutation(testShortCodeKey, domain)
		seen := make([]bool, domain)
		for x := range domain {
			y := permutation.apply(x)
			require.Less(t, y, domain, "domain %d: %d mapped outside the domain", domain, x)
			require.False(t, seen[y], "domain %d: %d is the image of two values", domain, y)
			seen[y] = true
		}
	}
}

func TestKeyedPermutation_DependsOnTheKey(t *testing.T) {
	first := newKeyedPermutation(testShortCodeKey, shortCodeSpace)
	second := newKeyedPermutation([]byte("a-different-key-that-is-also-32-bytes-long"), shortCodeSpace)

	same := 0
	for x := range uint64(100) {
		if first.apply(x) == second.apply(x) {
			same++
		}
	}
	assert.Zero(t, same, "another key must give other codes")
}

// The point of ADR-0019: consecutive counter values must not give codes that are close, ordered or share a prefix, or
// the next code could be guessed from the last.
func TestSequenceShortCodeGenerator_ConsecutiveCodesRevealNoOrder(t *testing.T) {
	gen := newTestGenerator(func(context.Context) (int64, error) { return 1, nil })

	var codes []string
	for range 200 {
		code, err := gen.Next(context.Background())
		require.NoError(t, err)
		require.Len(t, code.Value(), shortCodeLength)
		codes = append(codes, code.Value())
	}

	sharedPrefixes, ascending := 0, 0
	for i := 1; i < len(codes); i++ {
		if codes[i][:3] == codes[i-1][:3] {
			sharedPrefixes++
		}
		if codes[i] > codes[i-1] {
			ascending++
		}
	}
	// For random codes the chance that two neighbours share a 3-character prefix is 1/62^3; ascending pairs are about
	// half. Sequential codes would score 199 on both.
	assert.LessOrEqual(t, sharedPrefixes, 1, "consecutive codes share a prefix: %v", codes[:10])
	assert.InDelta(t, 100, ascending, 40, "consecutive codes are ordered: %v", codes[:10])
}

func TestSequenceShortCodeGenerator_IsDeterministicForAKey(t *testing.T) {
	first := newTestGenerator(func(context.Context) (int64, error) { return 1, nil })
	second := newTestGenerator(func(context.Context) (int64, error) { return 1, nil })

	for range 10 {
		a, err := first.Next(context.Background())
		require.NoError(t, err)
		b, err := second.Next(context.Background())
		require.NoError(t, err)
		assert.Equal(t, a, b)
	}
}

func TestSequenceShortCodeGenerator_ClaimsABlockOnlyWhenTheCurrentOneIsExhausted(t *testing.T) {
	claims := []int64{1, 5001}
	claimed := 0
	gen := newTestGenerator(func(context.Context) (int64, error) {
		start := claims[claimed]
		claimed++
		return start, nil
	})

	first, err := gen.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, toBase62(gen.permutation.apply(1)), first.Value())

	for range shortCodeBlockSize - 1 {
		_, err = gen.Next(context.Background())
		require.NoError(t, err)
	}
	assert.Equal(t, 1, claimed, "the first block covers %d codes", shortCodeBlockSize)

	next, err := gen.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, claimed)
	assert.Equal(t, toBase62(gen.permutation.apply(5001)), next.Value())
}

func TestSequenceShortCodeGenerator_ReturnsTheClaimError(t *testing.T) {
	gen := newTestGenerator(func(context.Context) (int64, error) {
		return 0, errors.New("database down")
	})

	_, err := gen.Next(context.Background())

	assert.ErrorContains(t, err, "database down")
}

func TestSequenceShortCodeGenerator_FailsOnceTheSpaceIsExhausted(t *testing.T) {
	gen := newTestGenerator(func(context.Context) (int64, error) { return shortCodeSpace - 1, nil })

	last, err := gen.Next(context.Background())
	require.NoError(t, err, "the last value in the space is still a valid code")
	assert.Len(t, last.Value(), shortCodeLength)

	_, err = gen.Next(context.Background())
	assert.ErrorIs(t, err, errShortCodeSpaceExhausted)
}

func TestSequenceShortCodeGenerator_IssuesUniqueCodesConcurrently(t *testing.T) {
	var next int64 = 1
	gen := newTestGenerator(func(context.Context) (int64, error) {
		start := next
		next += shortCodeBlockSize
		return start, nil
	})

	const workers, perWorker = 8, 500
	codes := make(chan string, workers*perWorker)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range perWorker {
				code, err := gen.Next(context.Background())
				assert.NoError(t, err)
				codes <- code.Value()
			}
		})
	}
	wg.Wait()
	close(codes)

	seen := map[string]struct{}{}
	for code := range codes {
		_, dup := seen[code]
		assert.False(t, dup, "duplicate code %s", code)
		seen[code] = struct{}{}
	}
	assert.Len(t, seen, workers*perWorker)
}
