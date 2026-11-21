package identitydatastore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

func NewUserReadDatastoreAdapter(dbClient database.PostgresDatabaseClient, config database.Config) identity.UserReadDatastore {
	repo := &userReadDatastoreAdapter{
		dbClient:  dbClient,
		logPrefix: "UserReadDatastoreAdapter",
		config:    config,
	}

	// Wire up the real transaction executor. This delegates to postgres.WithTransactionVoid,
	// which handles the pgxpool.Pool lifecycle. Tests override this field directly.
	repo.withTx = func(ctx context.Context, fn func(q postgresrepo.UserReadQuerier) error) error {
		return postgres.WithTransactionVoid(ctx, dbClient, func(qtx *postgresql.Queries) error {
			// *postgresql.Queries satisfies UserReadQuerier, so we can pass it straight through.
			return fn(qtx)
		})
	}

	return repo
}

// fetchOne runs a single-row lookup and maps the result, translating pgx.ErrNoRows to a NotFound error.
func (repo *userReadDatastoreAdapter) fetchOne(
	ctx context.Context,
	operation string,
	logAttrs []any,
	query func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (postgresql.User, error),
) (identity.User, error) {
	handlerLogPrefix := fmt.Sprintf("%s<%s>", repo.logPrefix, operation)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.%s", repo.logPrefix, operation), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (identity.User, error) {
			userModel, queryErr := query(ctx, qtx)
			if queryErr != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to retrieve User", handlerLogPrefix), append(logAttrs, "error", queryErr)...)
				if errors.Is(queryErr, pgx.ErrNoRows) {
					return identity.User{}, errdefs.NotFound(queryErr)
				}
				return identity.User{}, errdefs.BadRequest(queryErr)
			}

			return MapUserModelToEntity(userModel)
		})
}

func (repo *userReadDatastoreAdapter) FetchById(ctx context.Context, userId string) (identity.User, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchById> Fetching user by ID", repo.logPrefix), "id", userId)

	return repo.fetchOne(ctx, "FetchById", []any{"id", userId},
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (postgresql.User, error) {
			userUUID, userUUIDErr := postgres.StringToUUID(userId)
			if userUUIDErr != nil {
				return postgresql.User{}, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
			}
			row, err := qtx.QueryUserById(ctx, userUUID)
			return row.User, err
		})
}

func (repo *userReadDatastoreAdapter) FetchByUsername(ctx context.Context, username string) (identity.User, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByUsername> Fetching user by username", repo.logPrefix), "username", username)

	return repo.fetchOne(ctx, "FetchByUsername", []any{"username", username},
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (postgresql.User, error) {
			row, err := qtx.QueryUserByUsername(ctx, username)
			return row.User, err
		})
}

func (repo *userReadDatastoreAdapter) FetchByEmail(ctx context.Context, email string) (identity.User, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByEmail> Fetching user by email", repo.logPrefix), "email", email)

	return repo.fetchOne(ctx, "FetchByEmail", []any{"email", email},
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (postgresql.User, error) {
			row, err := qtx.QueryUserByEmail(ctx, email)
			return row.User, err
		})
}

func (repo *userReadDatastoreAdapter) FetchByVerificationToken(ctx context.Context, token string) (identity.User, error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByVerificationToken> Fetching user by verification token", repo.logPrefix))

	// The token is deliberately kept out of the log attributes: it is a bearer credential.
	return repo.fetchOne(ctx, "FetchByVerificationToken", nil,
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (postgresql.User, error) {
			row, err := qtx.QueryUserByVerificationToken(ctx, pgtype.Text{String: token, Valid: true})
			return row.User, err
		})
}

func (repo *userReadDatastoreAdapter) FetchAll(ctx context.Context, params common.RequestParams) (repository.FetchRecordsResponse[identity.User], error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchAll> Fetching users", repo.logPrefix), "params", params)

	return repo.fetchMany(ctx, "FetchAll", toQueryAllUsersParams(params, nil))
}

func (repo *userReadDatastoreAdapter) FetchByStatus(ctx context.Context, status identity.UserStatus) (repository.FetchRecordsResponse[identity.User], error) {
	slog.InfoContext(ctx, fmt.Sprintf("%s<FetchByStatus> Fetching users by status", repo.logPrefix), "status", status)

	return repo.fetchMany(ctx, "FetchByStatus", toQueryAllUsersParams(common.NewRequestParams(), &status))
}

// fetchMany runs the paginated users query and maps every row, returning the page and the total count.
func (repo *userReadDatastoreAdapter) fetchMany(ctx context.Context, operation string, queryParams postgresql.QueryAllUsersParams) (repository.FetchRecordsResponse[identity.User], error) {
	handlerLogPrefix := fmt.Sprintf("%s<%s>", repo.logPrefix, operation)

	return postgresrepo.Execute(ctx, repo.config, fmt.Sprintf("%s.%s", repo.logPrefix, operation), repo.withTx,
		func(ctx context.Context, qtx postgresrepo.UserReadQuerier) (repository.FetchRecordsResponse[identity.User], error) {
			rows, queryErr := qtx.QueryAllUsers(ctx, queryParams)
			if queryErr != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to retrieve users", handlerLogPrefix), "params", queryParams, "error", queryErr)
				return repository.FetchRecordsResponse[identity.User]{}, errdefs.BadRequest(queryErr)
			}

			users := make([]identity.User, 0, len(rows))
			var total int64
			for _, row := range rows {
				total = row.TotalRecords
				user, mapErr := MapUserModelToEntity(row.User)
				if mapErr != nil {
					return repository.FetchRecordsResponse[identity.User]{}, mapErr
				}
				users = append(users, user)
			}

			page := 1
			if queryParams.LimitBy > 0 {
				page = int(queryParams.CurrentOffset/queryParams.LimitBy) + 1
			}

			return repository.FetchRecordsResponse[identity.User]{
				Records: users,
				Total:   int(total),
				Page:    page,
				Size:    len(users),
			}, nil
		})
}

// toQueryAllUsersParams maps generic request params onto the sqlc-generated query params.
// A nil status means no status filter.
func toQueryAllUsersParams(params common.RequestParams, status *identity.UserStatus) postgresql.QueryAllUsersParams {
	queryParams := postgresql.QueryAllUsersParams{
		IncludeDeleted: params.IncludeDeleted,
		OrderBy:        string(params.OrderOption.OrderBy),
		SortOrder:      string(params.OrderOption.SortOrder),
		CurrentOffset:  int32(params.Offset),
		LimitBy:        int32(params.Limit),
	}
	if status != nil {
		queryParams.UserStatus = postgresql.NullUserStatus{UserStatus: postgresql.UserStatus(*status), Valid: true}
	}
	if params.DateRangeOption.DateFieldOption != "" {
		queryParams.DateField = pgtype.Text{String: string(params.DateRangeOption.DateFieldOption), Valid: true}
		queryParams.DateFrom = pgtype.Timestamp{Time: params.DateRangeOption.StartDate, Valid: !params.DateRangeOption.StartDate.IsZero()}
		queryParams.DateTo = pgtype.Timestamp{Time: params.DateRangeOption.EndDate, Valid: !params.DateRangeOption.EndDate.IsZero()}
	}
	return queryParams
}
