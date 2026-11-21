package postgresrepo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

// TxExecutor runs fn with a querier bound to a transaction. In production it wraps
// postgres.WithTransactionVoid; in tests it can be replaced with a function that calls the mock
// querier directly, bypassing the real database entirely.
type TxExecutor[Q any] func(ctx context.Context, fn func(q Q) error) error

// Execute runs fn inside a transaction, bounded by the configured operation timeout and retried
// according to the retry config. The context is checked before fn runs so a cancelled request
// never reaches the database.
func Execute[T any, Q any](
	ctx context.Context,
	config database.Config,
	operationName string,
	withTx TxExecutor[Q],
	fn func(ctx context.Context, qtx Q) (T, error),
) (T, error) {
	operationCtx, operationCancel := context.WithTimeout(ctx, config.Timeout())
	defer operationCancel()

	return recoveryutils.ExecuteWithRetry(
		operationCtx,
		func(retryCtx context.Context) (T, error) {
			var result T
			err := withTx(retryCtx, func(qtx Q) error {
				if ctxErr := retryCtx.Err(); ctxErr != nil {
					slog.ErrorContext(retryCtx, "Operation cancelled before validation with error", "error", ctxErr)
					return fmt.Errorf("operation cancelled before validation: %w", ctxErr)
				}
				r, fnErr := fn(retryCtx, qtx)
				if fnErr != nil {
					return fnErr
				}
				result = r
				return nil
			})
			return result, err
		},
		config.RetryConfig,
		operationName,
	)
}
