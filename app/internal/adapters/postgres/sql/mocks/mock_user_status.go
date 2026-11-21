package mockpostgresql

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
)

type MockUserStatusOption func(postgresql.UserStatus)

func MockUserStatus(status identity.UserStatus, options ...MockUserStatusOption) postgresql.UserStatus {
	id := entity.NewID()
	description := faker.Sentence()
	createdAt := time.Now()
	updatedAt := time.Now()
	deletedAt := time.Now()

	userStatus := postgresql.UserStatus{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		Name:        string(status),
		Description: pgtype.Text{String: description, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
		DeletedAt:   pgtype.Timestamptz{Time: deletedAt, Valid: true},
	}

	for _, opt := range options {
		opt(userStatus)
	}

	return userStatus
}

func WithUserStatusName(name identity.UserStatus) MockUserStatusOption {
	return func(u postgresql.UserStatus) {
		u.Name = string(name)
	}
}

func WithUserStatusDescription(description string) MockUserStatusOption {
	return func(u postgresql.UserStatus) {
		u.Description = pgtype.Text{String: description, Valid: true}
	}
}

func WithUserStatusCreatedAt(createdAt time.Time) MockUserStatusOption {
	return func(u postgresql.UserStatus) {
		u.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	}
}

func WithUserStatusUpdatedAt(updatedAt time.Time) MockUserStatusOption {
	return func(u postgresql.UserStatus) {
		u.UpdatedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
}

func WithUserStatusDeletedAt(deletedAt time.Time) MockUserStatusOption {
	return func(u postgresql.UserStatus) {
		u.DeletedAt = pgtype.Timestamptz{Time: deletedAt, Valid: true}
	}
}
