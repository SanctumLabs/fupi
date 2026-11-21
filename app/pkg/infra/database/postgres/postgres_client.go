package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"golang.org/x/exp/slog"
)

type (
	// PostgresDBConnString represents a connection string to a postgres database
	PostgresDBConnString string

	postgresClient struct {
		connAttempts int
		connTimeout  time.Duration

		db            *pgxpool.Pool
		logPrefix     string
		timeoutConfig TimeoutConfig
	}
)

// test our interface implementation
var _ database.PostgresDatabaseClient = (*postgresClient)(nil)

func PostgresDatabaseClientProvider(cfg PostgresDatabaseConfig) (database.PostgresDatabaseClient, func(), error) {
	postgresDbClient, err := NewPostgresClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return postgresDbClient, func() { postgresDbClient.Close() }, nil
}

// NewPostgresClient creates a postgres database client with a connection to a postgres database
func NewPostgresClient(config PostgresDatabaseConfig) (database.PostgresDatabaseClient, error) {
	ctx := context.Background()
	logPrefix := "PostgresClient"

	connStr := buildConnectionString(config)

	slog.InfoContext(ctx, fmt.Sprintf("%s> connecting to database...", logPrefix), "name", config.Name, "host", config.Host, "port", config.Port)

	pg := &postgresClient{
		connAttempts:  defaultConnAttempts,
		connTimeout:   defaultConnTimeout,
		logPrefix:     logPrefix,
		timeoutConfig: config.GetTimeoutConfig(),
	}

	// Configure connection pool
	poolConfig, poolConfigErr := pgxpool.ParseConfig(connStr)
	if poolConfigErr != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", poolConfigErr)
	}

	// Enhanced pool configuration
	poolConfig.MaxConns = config.MaxConns
	poolConfig.MinConns = config.MinConns
	poolConfig.MaxConnLifetime = config.MaxConnLifetime
	poolConfig.MaxConnIdleTime = config.MaxConnIdleTime

	// Health check configuration
	poolConfig.HealthCheckPeriod = time.Minute * 1

	// Connection timeout
	poolConfig.ConnConfig.ConnectTimeout = config.ConnTimeout

	// Use a local counter to avoid mutating the struct field
	attemptsLeft := pg.connAttempts
	var connectionErr error
	for attemptsLeft > 0 {
		// Use a timeout context for connection attempts
		connCtx, cancel := context.WithTimeout(ctx, config.ConnTimeout)
		pg.db, connectionErr = pgxpool.NewWithConfig(connCtx, poolConfig)
		cancel()

		if connectionErr != nil {
			slog.ErrorContext(ctx,
				fmt.Sprintf("%s> 🚫 Failed to create connection pool, attempts left: %d", logPrefix, attemptsLeft-1),
				"error", connectionErr,
			)
			time.Sleep(pg.connTimeout)
			attemptsLeft--
			continue
		}

		// Test the connection with timeout
		pingCtx, pingCancel := context.WithTimeout(ctx, config.ConnTimeout)
		connectionErr = pg.db.Ping(pingCtx)
		pingCancel()

		if connectionErr != nil {
			// Close the pool before retrying to avoid resource leaks
			pg.db.Close()
			pg.db = nil
			slog.WarnContext(ctx,
				fmt.Sprintf("%s> 🚫 failed to ping database, attempts left: %d", logPrefix, attemptsLeft-1),
				"error", connectionErr,
				"database", config.Name,
			)
			time.Sleep(pg.connTimeout)
			attemptsLeft--
			continue
		}
		break
	}

	if connectionErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s> 🚫 failed to connect to database, Error: %s", logPrefix, connectionErr), "error", connectionErr)
		return nil, connectionErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s> ✅ connected to DB", logPrefix),
		"name", config.Name,
		"host", config.Host,
		"port", config.Port,
		"active_conns", pg.db.Stat().AcquiredConns(),
		"idle_conns", pg.db.Stat().IdleConns(),
	)

	return pg, nil
}

func (pc *postgresClient) Configure(opts ...database.PostgresqlDbOption) database.PostgresDatabaseClient {
	for _, opt := range opts {
		opt(pc)
	}

	return pc
}

func (pc *postgresClient) WithConnAttempts(attempts int) {
	if attempts < 1 {
		attempts = 1
	}

	pc.connAttempts = attempts
}

func (pc *postgresClient) WithConnTimeout(timeout time.Duration) {
	if timeout < 0 {
		timeout = time.Second
	}
	pc.connTimeout = timeout
}

func (pc *postgresClient) GetDB() *pgxpool.Pool {
	return pc.db
}

func (pc *postgresClient) GetDBWithTimeout(ctx context.Context, operationType string) (*pgxpool.Pool, context.Context, context.CancelFunc) {
	if pc.db == nil {
		// Return a no-op cancel function to avoid nil pointer issues
		return nil, ctx, func() {}
	}
	timeoutCtx, cancel := pc.timeoutConfig.ContextWithTimeout(ctx, operationType)
	return pc.db, timeoutCtx, cancel
}

func (pc *postgresClient) Close() {
	if pc.db != nil {
		slog.InfoContext(context.Background(), fmt.Sprintf("%s> closing database connections", pc.logPrefix))
		pc.db.Close()
	}
}

// HealthCheck performs a health check on the database connection
func (pc *postgresClient) HealthCheck(ctx context.Context) error {
	if pc.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	healthCtx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	if err := pc.db.Ping(healthCtx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	stats := pc.db.Stat()
	if stats.AcquiredConns() == stats.MaxConns() {
		slog.WarnContext(ctx, fmt.Sprintf("%s> connection pool exhausted", pc.logPrefix),
			"acquired", stats.AcquiredConns(),
			"max", stats.MaxConns())
	}

	return nil
}
