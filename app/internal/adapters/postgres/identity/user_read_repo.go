package identityrepo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/ports/repository"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
)

func NewUserReadRepoAdapter(dbClient database.PostgresDatabaseClient, config database.Config) identity.UserReadRepository {
	repo := &userReadRepositoryAdapter{
		dbClient:  dbClient,
		logPrefix: "UserReadRepoAdapter",
		config:    config,
	}

	// Wire up the real transaction executor. This delegates to postgres.WithTransaction,
	// which handles the pgxpool.Pool lifecycle. Tests override this field directly.
	repo.withTx = func(ctx context.Context, fn func(q postgresrepo.UserReadQuerier) (identity.User, error)) (identity.User, error) {
		return postgres.WithTransaction(ctx, dbClient, func(qtx *postgresql.Queries) (identity.User, error) {
			// *postgresql.Queries satisfies userReadQuerier, so we can pass it straight through.
			return fn(qtx)
		})
	}

	return repo
}

func (repo *userReadRepositoryAdapter) FetchById(ctx context.Context, userId string) (identity.User, error) {
	handlerLogPrefix := fmt.Sprintf("%s<FetchById>", repo.logPrefix)
	slog.InfoContext(ctx, fmt.Sprintf("%s Fetching user by ID", handlerLogPrefix), "id", userId)

	operationCtx, operationCancel := context.WithTimeout(ctx, repo.config.OperationTimeout)
	defer operationCancel()

	return recoveryutils.ExecuteWithRetry(
		operationCtx,
		func(retryCtx context.Context) (identity.User, error) {
			return repo.withTx(retryCtx, func(qtx postgresrepo.UserReadQuerier) (identity.User, error) {
				select {
				case <-retryCtx.Done():
					slog.ErrorContext(retryCtx, "Operation cancelled before validation with error", "error", retryCtx.Err())
					return identity.User{}, fmt.Errorf("operation cancelled before validation: %w", retryCtx.Err())
				default:
				}
				userUUID, userUUIDErr := postgres.StringToUUID(userId)
				if userUUIDErr != nil {
					return identity.User{}, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
				}
				existingUser, existingUserErr := qtx.QueryUserById(retryCtx, userUUID)
				if existingUserErr != nil {
					slog.ErrorContext(
						retryCtx,
						fmt.Sprintf("%s Failed to retrieve User", handlerLogPrefix),
						"error", existingUserErr,
						"id", userId,
					)
					if errors.Is(existingUserErr, pgx.ErrNoRows) {
						return identity.User{}, errdefs.NotFound(existingUserErr)
					}

					return identity.User{}, errdefs.BadRequest(existingUserErr)
				}

				return MapUserModelToEntity(UserMapperParams{
					UserModel: existingUser.User,
					Status:    existingUser.UserStatus.Name,
				})
			})
		},
		repo.config.RetryConfig,
		fmt.Sprintf("%s.FetchById", repo.logPrefix),
	)
}

func (repo *userReadRepositoryAdapter) FetchAll(ctx context.Context, params common.RequestParams) (repository.FetchRecordsResponse[identity.User], error) {
	panic("not implemented")
}

func (repo *userReadRepositoryAdapter) FetchByUsername(ctx context.Context, username string) (identity.User, error) {
	panic("not implemented")
}

func (repo *userReadRepositoryAdapter) FetchByEmail(ctx context.Context, email string) (identity.User, error) {
	panic("not implemented")
}

func (repo *userReadRepositoryAdapter) FetchByStatus(ctx context.Context, status identity.UserStatus) (repository.FetchRecordsResponse[identity.User], error) {
	panic("not implemented")
}
