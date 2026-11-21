package mockpostgresql

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
)

type MockUrlStatusOption func(postgresql.UrlStatus)

func MockUrlStatus(status url.URLStatus, options ...MockUrlStatusOption) postgresql.UrlStatus {
	id := entity.NewID()
	description := faker.Sentence()
	createdAt := time.Now()
	updatedAt := time.Now()
	deletedAt := time.Now()

	urlStatus := postgresql.UrlStatus{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		Name:        string(status),
		Description: pgtype.Text{String: description, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
		DeletedAt:   pgtype.Timestamptz{Time: deletedAt, Valid: true},
	}

	for _, opt := range options {
		opt(urlStatus)
	}

	return urlStatus
}

// WithDescription updates the description of the url status
func WithName(name url.URLStatus) MockUrlStatusOption {
	return func(u postgresql.UrlStatus) {
		u.Name = string(name)
	}
}

func WithDescription(description string) MockUrlStatusOption {
	return func(u postgresql.UrlStatus) {
		u.Description = pgtype.Text{String: description, Valid: true}
	}
}

func WithUrlStatusCreatedAt(createdAt time.Time) MockUrlStatusOption {
	return func(u postgresql.UrlStatus) {
		u.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	}
}

func WithUrlStatusUpdatedAt(updatedAt time.Time) MockUrlStatusOption {
	return func(u postgresql.UrlStatus) {
		u.UpdatedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
}

func WithUrlStatusDeletedAt(deletedAt time.Time) MockUrlStatusOption {
	return func(u postgresql.UrlStatus) {
		u.DeletedAt = pgtype.Timestamptz{Time: deletedAt, Valid: true}
	}
}
