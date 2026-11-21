package outboxdatastore

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The sqlc parameters are int32 while the relay's settings are int: a value past the range must saturate, not wrap into a
// small or negative number that would silently change how many rows a claim or a purge touches.
func TestClampInt32_SaturatesInsteadOfWrapping(t *testing.T) {
	assert.Equal(t, int32(100), clampInt32(100))
	assert.Equal(t, int32(0), clampInt32(0))
	assert.Equal(t, int32(math.MaxInt32), clampInt32(math.MaxInt32))
	assert.Equal(t, int32(math.MaxInt32), clampInt32(math.MaxInt32+1), "2147483648 does not become a negative number")
	assert.Equal(t, int32(math.MaxInt32), clampInt32(1<<32+5), "4294967301 does not become 5")
	assert.Equal(t, int32(math.MinInt32), clampInt32(math.MinInt32-1))
}
