package database

import (
	"time"

	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

// DefaultOperationTimeout is used when Config.OperationTimeout is not set, so a zero-value config
// never produces an already-expired context.
const DefaultOperationTimeout = 30 * time.Second

// Config is the base configuration for handling database connections. This can be used by database implementations to configure how
// database transactions are created and handled or how retry mechanism can be handled.
type Config struct {
	// OperationTimeout is how long a database timeout is
	OperationTimeout time.Duration

	// RetryConfig retry configuration for the database operation
	RetryConfig recoveryutils.RetryConfig
}

// Timeout returns OperationTimeout, falling back to DefaultOperationTimeout when it is not set.
func (c Config) Timeout() time.Duration {
	if c.OperationTimeout <= 0 {
		return DefaultOperationTimeout
	}
	return c.OperationTimeout
}
