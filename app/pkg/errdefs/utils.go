package errdefs

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// IsRetryableError checks if an error is retryable.
func IsRetryableError(err error) bool {
	// Implement logic to determine if an error is retryable
	// For example, network timeouts, rate limiting errors, etc.
	if err == nil {
		return false
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for context canceled (don't retry if user canceled)
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Example: Check if it's a temporary network error or rate limit
	// Network errors are typically retryable
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	if strings.Contains(err.Error(), "rate limit exceeded") {
		return false
	}

	// Context timeout errors are retryable in some cases
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// System call errors
	var syscallErr *net.OpError
	if errors.As(err, &syscallErr) {
		if syscallErr.Err == syscall.ECONNREFUSED ||
			syscallErr.Err == syscall.ECONNRESET ||
			syscallErr.Err == syscall.ETIMEDOUT {
			return true
		}
	}

	// PostgreSQL specific errors
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001": // serialization_failure
		case "40P01": // deadlock_detected
		case "53000": // insufficient_resources
		case "53100": // disk_full
		case "53200": // out_of_memory
		case "53300": // too_many_connections
		case "54000": // program_limit_exceeded
		case "54001": // statement_too_complex
		case "54011": // too_many_columns
		case "54023": // too_many_arguments
			return true
		case "23505": // unique_violation - might be retryable depending on business logic
			return false
		default:
			return false
		}
	}

	// Connection errors from pgx
	if errors.Is(err, pgx.ErrTxClosed) ||
		errors.Is(err, pgx.ErrTxCommitRollback) {
		return false // These are not retryable
	}

	return false // Default to not retryable
}

// IsDeadlockError checks if the error is a PostgreSQL deadlock
func IsDeadlockError(err error) bool {
	if err == nil {
		return false
	}

	// Check for PostgreSQL deadlock error code
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "40P01" // deadlock_detected
	}

	// Also check error message as fallback
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "deadlock") ||
		strings.Contains(errMsg, "40p01") ||
		strings.Contains(errMsg, "could not serialize access")
}

func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common timeout error patterns
	errStr := err.Error()
	timeoutPatterns := []string{
		"context deadline exceeded",
		"timeout",
		"connection timeout",
		"i/o timeout",
	}

	for _, pattern := range timeoutPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
