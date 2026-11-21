package recoveryutils

import (
	"context"
	"time"
)

type RetryConfig struct {
	MaxAttempts     int
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	BackoffExponent float64
	JitterPercent   float64
}

var DefaultRetryConfig = RetryConfig{
	MaxAttempts:     3,
	BaseDelay:       100 * time.Millisecond,
	MaxDelay:        5 * time.Second,
	BackoffExponent: 2.0,
	JitterPercent:   0.1,
}

var CriticalRetryConfig = RetryConfig{
	MaxAttempts:     5,
	BaseDelay:       50 * time.Millisecond,
	MaxDelay:        3 * time.Second,
	BackoffExponent: 1.5,
	JitterPercent:   0.2,
}

// RetryableOperation represents an operation that can be retried
type RetryableOperation[T any] func(ctx context.Context) (T, error)
