package mockpostgresql

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
)

type MockUrlOption func(*postgresql.Url)

func MockUrl(options ...MockUrlOption) postgresql.Url {
	id := entity.NewID()
	userId := entity.NewID()
	shortCode := faker.Word()
	customAlias := faker.Word()
	ogTitle := faker.Word()
	ogDescription := faker.Word()
	ogImageUrl := faker.URL()
	originalUrl := faker.URL()
	statusId := entity.NewID()
	metadata := []byte{}
	expiresOn := time.Now()
	createdAt := time.Now()
	updatedAt := time.Now()
	deletedAt := time.Now()

	urlModel := &postgresql.Url{
		ID:            pgtype.UUID{Bytes: id, Valid: true},
		UserID:        pgtype.UUID{Bytes: userId, Valid: true},
		ShortCode:     shortCode,
		CustomAlias:   pgtype.Text{String: customAlias, Valid: true},
		OriginalUrl:   originalUrl,
		StatusID:      pgtype.UUID{Bytes: statusId, Valid: true},
		OgTitle:       pgtype.Text{String: ogTitle, Valid: true},
		OgDescription: pgtype.Text{String: ogDescription, Valid: true},
		OgImageUrl:    pgtype.Text{String: ogImageUrl, Valid: true},
		Metadata:      metadata,
		ExpiresOn:     pgtype.Timestamptz{Time: expiresOn, Valid: true},
		CreatedAt:     pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:     pgtype.Timestamptz{Time: updatedAt, Valid: true},
		DeletedAt:     pgtype.Timestamptz{Time: deletedAt, Valid: true},
	}

	for _, opt := range options {
		opt(urlModel)
	}

	return *urlModel
}

func WithUserId(userId entity.ID) MockUrlOption {
	return func(u *postgresql.Url) {
		u.UserID = pgtype.UUID{Bytes: userId, Valid: true}
	}
}

func WithShortCode(shortCode string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.ShortCode = shortCode
	}
}

func WithCustomAlias(customAlias string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.CustomAlias = pgtype.Text{String: customAlias, Valid: true}
	}
}

func WithStatusId(statusId entity.ID) MockUrlOption {
	return func(u *postgresql.Url) {
		u.StatusID = pgtype.UUID{Bytes: statusId, Valid: true}
	}
}

func WithOriginalUrl(originalUrl string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.OriginalUrl = originalUrl
	}
}

func WithOgTitle(title string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.OgTitle = pgtype.Text{String: title, Valid: true}
	}
}

func WithOgDescription(description string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.OgDescription = pgtype.Text{String: description, Valid: true}
	}
}

func WithOgImageUrl(ogImageUrl string) MockUrlOption {
	return func(u *postgresql.Url) {
		u.OgImageUrl = pgtype.Text{String: ogImageUrl, Valid: true}
	}
}

func WithMetadata(metadata []byte) MockUrlOption {
	return func(u *postgresql.Url) {
		u.Metadata = metadata
	}
}

func WithExpiresOn(expiresOn time.Time) MockUrlOption {
	return func(u *postgresql.Url) {
		u.ExpiresOn = pgtype.Timestamptz{Time: expiresOn, Valid: true}
	}
}

func WithUrlCreatedAt(createdAt time.Time) MockUrlOption {
	return func(u *postgresql.Url) {
		u.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	}
}

func WithUrlUpdatedAt(updatedAt time.Time) MockUrlOption {
	return func(u *postgresql.Url) {
		u.UpdatedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
}

func WithUrlDeletedAt(deletedAt time.Time) MockUrlOption {
	return func(u *postgresql.Url) {
		u.DeletedAt = pgtype.Timestamptz{Time: deletedAt, Valid: true}
	}
}

// WithUrl maps a mock url to Url Model
func WithUrl(mockUrl url.URL) MockUrlOption {
	return func(u *postgresql.Url) {
		u.UserID = pgtype.UUID{Bytes: mockUrl.UserId(), Valid: true}
		u.ShortCode = mockUrl.ShortCode().Value()
		u.CustomAlias = pgtype.Text{String: mockUrl.CustomAlias().Value(), Valid: true}
		u.OriginalUrl = mockUrl.OriginalURL().Value()
		u.ExpiresOn = pgtype.Timestamptz{Time: mockUrl.ExpiresOn(), Valid: true}
		u.OgImageUrl = pgtype.Text{String: mockUrl.OgImageUrl(), Valid: true}
		u.OgTitle = pgtype.Text{String: mockUrl.OgTitle(), Valid: true}
		u.OgDescription = pgtype.Text{String: mockUrl.OgDescription(), Valid: true}
	}
}
