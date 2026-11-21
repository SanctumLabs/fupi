package identitydatastore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

// queryUserById is a helper function to query a user by ID
func queryUserById(ctx context.Context, qtx postgresrepo.UserReadQuerier, userId string) (*postgresql.QueryUserByIdRow, error) {
	userUUID, userUUIDErr := postgres.StringToUUID(userId)
	if userUUIDErr != nil {
		return nil, fmt.Errorf("failed to convert user ID to UUID: %w", userUUIDErr)
	}
	existingUser, existingUserErr := qtx.QueryUserById(ctx, userUUID)
	if existingUserErr != nil {
		slog.ErrorContext(
			ctx,
			"Failed to retrieve user",
			"id", userId,
			"error", existingUserErr,
		)
		if errors.Is(existingUserErr, pgx.ErrNoRows) {
			return nil, errdefs.NotFound(existingUserErr)
		}

		return nil, fmt.Errorf("failed to query user: %s %w", userId, existingUserErr)
	}
	return &existingUser, nil
}

// uniqueViolationCode is PostgreSQL's SQLSTATE for a unique constraint violation.
const uniqueViolationCode = "23505"

// asConflict maps a unique-constraint violation onto a Conflict error so callers (and the HTTP
// layer) can tell "this username/email is taken" apart from a genuine failure. Any other error is
// returned unchanged.
func asConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return errdefs.Conflict(err)
	}
	return err
}
