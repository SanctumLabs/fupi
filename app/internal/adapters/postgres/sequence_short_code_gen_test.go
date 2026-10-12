package postgresrepo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToBase62(t *testing.T) {
	cases := map[int64]string{
		1:         "000001",
		61:        "00000Z",
		62:        "000010",
		916132831: "0ZZZZZ", // 62^5 - 1, the last code that needs padding
		916132832: "100000", // 62^5
	}

	for n, want := range cases {
		assert.Equal(t, want, toBase62(n), "toBase62(%d)", n)
	}
}

func TestSequenceShortCodeGenerator_ClaimsABlockOnlyWhenTheCurrentOneIsExhausted(t *testing.T) {
	claims := []int64{1, 5001}
	claimed := 0
	gen := &sequenceShortCodeGenerator{claimBlock: func(context.Context) (int64, error) {
		start := claims[claimed]
		claimed++
		return start, nil
	}}

	first, err := gen.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "000001", first.Value())

	for range shortCodeBlockSize - 1 {
		_, err = gen.Next(context.Background())
		require.NoError(t, err)
	}
	assert.Equal(t, 1, claimed, "the first block covers %d codes", shortCodeBlockSize)

	next, err := gen.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, claimed)
	assert.Equal(t, toBase62(5001), next.Value())
}

func TestSequenceShortCodeGenerator_ReturnsTheClaimError(t *testing.T) {
	gen := &sequenceShortCodeGenerator{claimBlock: func(context.Context) (int64, error) {
		return 0, errors.New("database down")
	}}

	_, err := gen.Next(context.Background())

	assert.ErrorContains(t, err, "database down")
}

func TestSequenceShortCodeGenerator_IssuesUniqueCodesConcurrently(t *testing.T) {
	var next int64 = 1
	gen := &sequenceShortCodeGenerator{claimBlock: func(context.Context) (int64, error) {
		start := next
		next += shortCodeBlockSize
		return start, nil
	}}

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
