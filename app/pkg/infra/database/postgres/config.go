package postgres

import (
	"context"
	"time"
)

// PostgresDatabaseConfig provides parameter options used to create a new postgres database client
type (
	PostgresDatabaseConfig struct {
		Host     string `env-description:"Database Host" yaml:"host" env:"DATABASE_HOST" env-default:"localhost"`
		Username string `env-description:"Database Username" yaml:"username" env:"DATABASE_USERNAME" env-default:"curtz-user"`
		Password string `env-description:"Database Password" yaml:"password" env:"DATABASE_PASSWORD" env-default:"curtz-pass"`
		Name     string `env-description:"Database Name" yaml:"database" env:"DATABASE_NAME" env-default:"curtzdb"`
		Port     string `env-description:"Database Port" yaml:"port" env:"DATABASE_PORT" env-default:"5433"`
		Url      string `env-description:"Database URL" yaml:"url" env:"DATABASE_URL" env-default:"postgres://curtz-user:curtz-pass@localhost:5433/curtzdb?sslmode=disable"`
		Schema   string `env-description:"Database Schema" yaml:"schema" env:"DATABASE_SCHEMA" env-default:"bid"`
		SslMode  string `env-description:"Database SSL Mode" yaml:"ssl_mode" env:"DATABASE_SSL_MODE" env-default:"disable"`

		// Enhanced connection pool configuration
		MaxConns        int32         `env-description:"Maximum number of connections" yaml:"max_conns" env:"DATABASE_MAX_CONNS" env-default:"30"`
		MinConns        int32         `env-description:"Minimum number of connections" yaml:"min_conns" env:"DATABASE_MIN_CONNS" env-default:"5"`
		MaxConnLifetime time.Duration `env-description:"Maximum connection lifetime" yaml:"max_conn_lifetime" env:"DATABASE_MAX_CONN_LIFETIME" env-default:"1h"`
		MaxConnIdleTime time.Duration `env-description:"Maximum connection idle time" yaml:"max_conn_idle_time" env:"DATABASE_MAX_CONN_IDLE_TIME" env-default:"30m"`

		// Timeout configurations
		ConnTimeout  time.Duration `env-description:"Connection timeout" yaml:"conn_timeout" env:"DATABASE_CONN_TIMEOUT" env-default:"30s"`
		QueryTimeout time.Duration `env-description:"Query timeout" yaml:"query_timeout" env:"DATABASE_QUERY_TIMEOUT" env-default:"10s"`
	}

	// TimeoutConfig holds different timeout values for different operation types
	TimeoutConfig struct {
		DefaultTimeout  time.Duration
		CriticalTimeout time.Duration // For critical operations like bid creation
		ReadTimeout     time.Duration // For read operations
		BulkTimeout     time.Duration // For bulk operations
	}
)

// GetTimeoutConfig returns timeout configuration based on operation criticality
func (c PostgresDatabaseConfig) GetTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		DefaultTimeout:  c.QueryTimeout,
		CriticalTimeout: c.QueryTimeout * 2, // Double timeout for critical operations
		ReadTimeout:     c.QueryTimeout / 2, // Half timeout for reads
		BulkTimeout:     c.QueryTimeout * 3, // Triple timeout for bulk operations
	}
}

// ContextWithTimeout creates a context with appropriate timeout for the operation type
func (tc TimeoutConfig) ContextWithTimeout(parent context.Context, operationType string) (context.Context, context.CancelFunc) {
	var timeout time.Duration

	switch operationType {
	case OperationTypeCritical:
		timeout = tc.CriticalTimeout
	case OperationTypeRead:
		timeout = tc.ReadTimeout
	case OperationTypeBulk:
		timeout = tc.BulkTimeout
	default:
		timeout = tc.DefaultTimeout
	}

	return context.WithTimeout(parent, timeout)
}
