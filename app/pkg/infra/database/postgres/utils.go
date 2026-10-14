package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

// ConnectionString returns the DSN for a database config. DATABASE_URL (Url) wins when set; otherwise the DSN is
// built from the parts with the credentials escaped, so a password containing @ / : ? # or % cannot corrupt it.
func ConnectionString(config PostgresDatabaseConfig) string {
	if config.Url != "" {
		return config.Url
	}

	query := url.Values{}
	if config.SslMode != "" {
		query.Set("sslmode", config.SslMode)
	}
	query.Set("pool_max_conns", strconv.Itoa(int(config.MaxConns)))
	query.Set("pool_min_conns", strconv.Itoa(int(config.MinConns)))

	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(config.Username, config.Password),
		Host:     net.JoinHostPort(config.Host, config.Port),
		Path:     "/" + config.Name,
		RawQuery: query.Encode(),
	}
	return dsn.String()
}

// WithTransactionRetry wraps a transaction with retry logic
func WithTransactionRetry[T any](
	ctx context.Context,
	db *pgxpool.Pool,
	operation func(tx pgx.Tx) (T, error),
	config recoveryutils.RetryConfig,
	operationName string,
) (T, error) {
	return recoveryutils.ExecuteWithRetry(ctx, func(ctx context.Context) (T, error) {
		tx, err := db.Begin(ctx)
		if err != nil {
			var result T
			return result, fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }() // This is safe even if commit succeeds

		result, err := operation(tx)
		if err != nil {
			return result, err
		}

		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return result, nil
	}, config, operationName)
}

// QueryRetriever retrieves queries from the given database client
func QueryRetriever(dbClient database.PostgresDatabaseClient) *postgresql.Queries {
	db := dbClient.GetDB()
	querier := postgresql.New(db)
	return querier
}

// WithTransaction is a convenience wrapper utility function that retrieves database queries from the database client
// and begins a database transaction from a given context. If an error is encountered an error is returned, otherwise
// the transaction is committed. This allows functions that require committing a database transaction to do it automatically.
// In the event of a failure, the transaction will automatically rollback
func WithTransaction[T any](ctx context.Context, dbClient database.PostgresDatabaseClient, fn func(*postgresql.Queries) (T, error)) (T, error) {
	db := dbClient.GetDB()
	tx, err := db.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Database: failed to start transaction for db connection", "error", err)
		return *new(T), err
	}

	defer func() {
		if err != nil {
			slog.ErrorContext(ctx, "Database: failed committing transaction", "error", err)

			err = tx.Rollback(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Database: failed to rollback tx", "error", err)
			}
		}
	}()

	querier := QueryRetriever(dbClient)

	qtx := querier.WithTx(tx)

	// perform database query/execution
	record, err := fn(qtx)
	if err != nil {
		slog.ErrorContext(ctx, "Database: failed to execute tx", "error", err)
		return *new(T), err
	}

	// commit transaction
	return record, tx.Commit(ctx)
}

// WithTransactionVoid is a convenience wrapper utility function that retrieves database queries from the database client
// and begins a database transaction from a given context. This does the same thing as the WithTransaction function, but only returns either an error or nil
func WithTransactionVoid(ctx context.Context, dbClient database.PostgresDatabaseClient, fn func(*postgresql.Queries) error) error {
	db := dbClient.GetDB()
	tx, err := db.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Database: failed to start transaction for db connection", "error", err)
		return err
	}

	defer func() {
		if err != nil {
			slog.ErrorContext(ctx, "Database: failed committing transaction", "error", err)

			err = tx.Rollback(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Database: failed to rollback tx", "error", err)
			}
		}
	}()

	querier := QueryRetriever(dbClient)

	qtx := querier.WithTx(tx)

	// perform database query/execution
	err = fn(qtx)
	if err != nil {
		slog.ErrorContext(ctx, "Database: failed to execute tx", "error", err)
		return err
	}

	// commit transaction
	return tx.Commit(ctx)
}

func WithTransactionOptions[T any](ctx context.Context, dbClient database.PostgresDatabaseClient, txOptions pgx.TxOptions, fn func(*postgresql.Queries) (T, error)) (T, error) {
	db := dbClient.GetDB()
	tx, err := db.BeginTx(ctx, txOptions)
	slog.DebugContext(ctx, "database begin tx", "tx", tx)
	if err != nil {
		return *new(T), nil
	}

	defer func() {
		if err != nil {
			err = tx.Rollback(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "repository: failed to rollback tx", "err", err)
			}
		}
	}()

	querier := QueryRetriever(dbClient)

	qtx := querier.WithTx(tx)

	// perform database query/execution
	record, err := fn(qtx)
	if err != nil {
		return *new(T), err
	}

	// commit transaction
	slog.DebugContext(ctx, "committing transaction", "tx", tx)

	return record, tx.Commit(ctx)
}

// WithSqlQueryExec is a convenience wrapper utility function that executes database queries from the database client
func WithSqlQueryExec(ctx context.Context, dbClient database.PostgresDatabaseClient, sqlStr string, args []any) (pgconn.CommandTag, error) {
	db := dbClient.GetDB()
	cmdTag, err := db.Exec(ctx, sqlStr, args)

	return cmdTag, err
}

// WithSqlQueryRows is a convenience wrapper utility function that returns rows from a database sql query
func WithSqlQueryRows[T any](ctx context.Context, dbClient database.PostgresDatabaseClient, sqlStr string, args []any) ([]T, error) {
	db := dbClient.GetDB()
	rows, err := db.Query(ctx, sqlStr, args)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resultSet, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		slog.ErrorContext(
			ctx,
			fmt.Sprintf("%s CollectRows error", "WithSqlQueryRows"),
			"err", err,
		)
		return nil, err
	}

	return resultSet, err
}

// WithSqlQueryRow is a convenience wrapper utility function that returns at most one row from a database sql query
func WithSqlQueryRow[T any](ctx context.Context, dbClient database.PostgresDatabaseClient, sqlStr string, args []any, rowType T) (T, error) {
	db := dbClient.GetDB()
	row := db.QueryRow(ctx, sqlStr, args)
	err := row.Scan(rowType)
	if err != nil {
		return *new(T), err
	}

	return rowType, nil
}

// WithBatch is a convenience wrapper utility function that sends a batch of queries to the database and returns the batch results
func WithBatch[T any](ctx context.Context, dbClient database.PostgresDatabaseClient, batch *pgx.Batch) pgx.BatchResults {
	db := dbClient.GetDB()
	batchResults := db.SendBatch(ctx, batch)

	return batchResults
}
