package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type (
	// PostgresDatabaseClient is an interface containing a method set on handling an SQL a database engine
	PostgresDatabaseClient interface {
		// GetDB gets the connection handle instance
		GetDB() *pgxpool.Pool

		GetDBWithTimeout(ctx context.Context, operationType string) (*pgxpool.Pool, context.Context, context.CancelFunc)

		// Close closes the connection to a database
		Close()

		Configure(...PostgresqlDbOption) PostgresDatabaseClient

		WithConnAttempts(int)

		WithConnTimeout(time.Duration)

		HealthCheck(ctx context.Context) error
	}
)
