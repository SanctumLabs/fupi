package recoveryutils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime/debug"
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

// PanicHandlerOption represents an option for configuring panic handlers
type PanicHandlerOption func(*panicHandlerConfig)

type panicHandlerConfig struct {
	// Whether to log the panic
	shouldLog bool
	// Custom logger function (if nil, uses slog.Error)
	logger func(msg string, err error, stack []byte)
	// Function to convert panic value to error (if nil, uses default conversion)
	errorConverter func(interface{}) error
}

// WithCustomLogger specifies a custom logging function
func WithCustomLogger(logger func(msg string, err error, stack []byte)) PanicHandlerOption {
	return func(c *panicHandlerConfig) {
		c.shouldLog = true
		c.logger = logger
	}
}

func WithoutLogging() PanicHandlerOption {
	return func(c *panicHandlerConfig) {
		c.shouldLog = false
	}
}

// WithCustomErrorConverter specifies a custom function to convert panic values to errors
func WithCustomErrorConverter(converter func(interface{}) error) PanicHandlerOption {
	return func(c *panicHandlerConfig) {
		c.errorConverter = converter
	}
}

// defaultErrorConverter converts panic values to errors using a standard format
func defaultErrorConverter(r interface{}) error {
	switch v := r.(type) {
	case error:
		return fmt.Errorf("panic occurred: %w", v)
	default:
		return fmt.Errorf("panic occurred: %v", v)
	}
}

func defaultLogger(msg string, err error, stack []byte) {
	slog.Error(msg, "error", err, "stack", string(stack))
}

// HandlePanic provides a reusable panic recovery mechanism
// If errorPtr is non-nil, it will be set to the recovered error
func HandlePanic(errorPtr *error, options ...PanicHandlerOption) {
	// Initialize config with defaults
	config := panicHandlerConfig{
		shouldLog:      true,
		logger:         defaultLogger,
		errorConverter: defaultErrorConverter,
	}

	// Apply options
	for _, option := range options {
		option(&config)
	}

	// The actual recovery function
	if r := recover(); r != nil {
		// Capture stack trace
		stackTrace := debug.Stack()

		// Convert to error
		panicErr := config.errorConverter(r)

		// Log if enabled
		if config.shouldLog {
			config.logger("Recovered from panic", panicErr, stackTrace)
		}

		// Set error if pointer provided
		if errorPtr != nil {
			*errorPtr = panicErr
		}
	}
}

// SafeFunc executes any function with any arguments safely
// It works with functions that return (T, error) or just T
func SafeFunc[T any, Args any](fn func(Args) (T, error)) func(Args) (T, error) {
	return func(args Args) (result T, err error) {
		defer HandlePanic(&err)
		return fn(args)
	}
}

// SafeFuncValue executes any function that returns only a value (no error)
func SafeFuncValue[T any, Args any](fn func(Args) T) func(Args) (T, error) {
	return func(args Args) (result T, err error) {
		defer HandlePanic(&err)
		result = fn(args)
		return result, nil
	}
}

func SafeFuncError[Args any](fn func(Args) error) func(Args) error {
	return func(args Args) (err error) {
		defer HandlePanic(&err)
		return fn(args)
	}
}

func SafeFuncVoid[Args any](fn func(Args)) func(Args) error {
	return func(args Args) (err error) {
		defer HandlePanic(&err)
		fn(args)
		return nil
	}
}

func SafeFuncExec(fn func()) func() {
	return func() {
		defer HandlePanic(nil)
		fn()
	}
}

// SafeExec executes any function with any arguments safely without returning an error
// Use this when you want to catch panics but don't care about error propagation
func SafeExec[T any, Args any](fn func(Args) T) func(Args) T {
	return func(args Args) (result T) {
		defer HandlePanic(nil)
		return fn(args)
	}
}

// WithRetry runs operation once, then retries up to maxRetries times with exponential backoff
// when the error indicates a timeout (e.g., context.DeadlineExceeded). Returns the first
// non-timeout error immediately, or ctx.Err() if the context is canceled while waiting.
func WithRetry(
	ctx context.Context,
	maxRetries int,
	backoff time.Duration,
	operation func() error,
) error {
	if operation == nil {
		return errors.New("operation cannot be nil")
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}
		// Fast-fail on non-timeout errors or when out of retries
		if !IsTimeoutError(err) || attempt == maxRetries {
			return err
		}
		// Backoff or exit if context done
		select {
		case <-time.After(backoff):
			if backoff > 0 {
				backoff *= 2
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// IsTimeoutError reports whether err is a timeout.
func IsTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	type timeoutErr interface{ Timeout() bool }
	var te timeoutErr
	return errors.As(err, &te) && te.Timeout()
}

// ExecuteWithRetry executes an operation with retry logic
func ExecuteWithRetry[T any](
	ctx context.Context,
	operation RetryableOperation[T],
	config RetryConfig,
	operationName string,
) (T, error) {
	var lastErr error
	var result T

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("operation cancelled during retry attempt %d: %w", attempt, ctx.Err())
		default:
		}

		result, lastErr = operation(ctx)
		if lastErr == nil {
			if attempt > 1 {
				slog.InfoContext(ctx, "Operation succeeded after retry",
					"operation", operationName,
					"attempt", attempt,
					"total_attempts", config.MaxAttempts)
			}
			return result, nil
		}

		if !errdefs.IsRetryableError(lastErr) {
			slog.ErrorContext(
				ctx,
				"Non-retryable error encountered",
				"operation", operationName,
				"attempt", attempt,
				"error", lastErr,
			)
			return result, fmt.Errorf("non-retryable error in %s: %w", operationName, lastErr)
		}

		if attempt < config.MaxAttempts {
			delay := config.CalculateBackoffDelay(attempt)
			slog.WarnContext(
				ctx,
				"Operation failed, retrying",
				"operation", operationName,
				"attempt", attempt,
				"total_attempts", config.MaxAttempts,
				"delay", delay,
				"error", lastErr,
			)

			select {
			case <-ctx.Done():
				return result, fmt.Errorf("operation cancelled during retry delay: %w", ctx.Err())
			case <-time.After(delay):
				// Continue to next attempt
			}
		}
	}

	slog.ErrorContext(
		ctx,
		"Operation failed after all retry attempts",
		"operation", operationName,
		"total_attempts", config.MaxAttempts,
		"final_error", lastErr,
	)

	return result, fmt.Errorf("operation %s failed after %d attempts: %w",
		operationName, config.MaxAttempts, lastErr)
}

// CalculateBackoffDelay calculates the delay for the next retry attempt
func (rc RetryConfig) CalculateBackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return rc.BaseDelay
	}

	// Exponential backoff
	delay := float64(rc.BaseDelay) * math.Pow(rc.BackoffExponent, float64(attempt-1))

	// Cap at max delay
	if delay > float64(rc.MaxDelay) {
		delay = float64(rc.MaxDelay)
	}

	// Add jitter to prevent thundering herd
	jitter := delay * rc.JitterPercent * (rand.Float64()*2 - 1) // +/- jitter percent
	delay += jitter

	// Ensure delay is not negative
	if delay < 0 {
		delay = float64(rc.BaseDelay)
	}

	return time.Duration(delay)
}
