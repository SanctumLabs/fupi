package urldatastore

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// uniqueViolationCode is PostgreSQL's SQLSTATE for a unique constraint violation.
const uniqueViolationCode = "23505"

// originalUrlUniqueIndex is the partial unique index enforcing that a target URL is shortened only
// once across all users. See migration 000002_unique_original_url.
const originalUrlUniqueIndex = "idx_urls_original_url"

// asConflict maps a unique-constraint violation on the urls table onto a Conflict error, so callers
// (and the HTTP layer) can tell a collision apart from a genuine failure.
//
// A violation of the original_url index is the global uniqueness rule being enforced, and carries
// ErrURLAlreadyExists so the caller can report it precisely. Collisions on short_code or
// custom_alias are different failures and are reported as a bare conflict.
//
// It returns nil when err is not a unique violation, leaving the caller to handle it.
func asConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolationCode {
		return nil
	}

	if pgErr.ConstraintName == originalUrlUniqueIndex {
		return errdefs.Conflict(fmt.Errorf("%w: %w", errdefs.ErrURLAlreadyExists, err))
	}

	return errdefs.Conflict(err)
}
