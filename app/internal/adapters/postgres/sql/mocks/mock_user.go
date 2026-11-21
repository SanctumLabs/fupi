package mockpostgresql

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
)

type MockUserOption func(*postgresql.User)

// MockUser creates a mock user with default values and applies any provided options to customize the user.
func MockUser(options ...MockUserOption) postgresql.User {
	id := entity.NewID()
	username := faker.Username()
	firstName := faker.FirstName()
	lastName := faker.LastName()
	email := faker.Email()
	passwordHash := faker.Password()
	verificationToken := faker.UUIDHyphenated()
	verificaitonExpires := time.Now().Add(24 * time.Hour)
	statusId := entity.NewID()
	metadata := []byte{}
	createdAt := time.Now()
	updatedAt := time.Now()
	deletedAt := time.Now()

	userModel := &postgresql.User{
		ID:                  pgtype.UUID{Bytes: id, Valid: true},
		Username:            username,
		FirstName:           pgtype.Text{String: firstName, Valid: true},
		LastName:            pgtype.Text{String: lastName, Valid: true},
		Email:               email,
		PasswordHash:        passwordHash,
		Verified:            true,
		VerificationToken:   pgtype.Text{String: verificationToken, Valid: true},
		VerificationExpires: pgtype.Timestamptz{Time: verificaitonExpires, Valid: true},
		StatusID:            pgtype.UUID{Bytes: statusId, Valid: true},
		Metadata:            metadata,
		CreatedAt:           pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: updatedAt, Valid: true},
		DeletedAt:           pgtype.Timestamptz{Time: deletedAt, Valid: true},
	}

	for _, opt := range options {
		opt(userModel)
	}

	return *userModel
}

// UserWithUsername sets the username
func UserWithUsername(username string) MockUserOption {
	return func(u *postgresql.User) {
		u.Username = username
	}
}

func UserWithFirstName(firstName string) MockUserOption {
	return func(u *postgresql.User) {
		u.FirstName = pgtype.Text{String: firstName, Valid: true}
	}
}

func UserWithLastName(lastName string) MockUserOption {
	return func(u *postgresql.User) {
		u.LastName = pgtype.Text{String: lastName, Valid: true}
	}
}

func UserWithEmail(email string) MockUserOption {
	return func(u *postgresql.User) {
		u.Email = email
	}
}

func UserWithPasswordHash(passwordHash string) MockUserOption {
	return func(u *postgresql.User) {
		u.PasswordHash = passwordHash
	}
}

func UserWithStatusId(statusId entity.ID) MockUserOption {
	return func(u *postgresql.User) {
		u.StatusID = pgtype.UUID{Bytes: statusId, Valid: true}
	}
}

func UserWithMetadata(metadata []byte) MockUserOption {
	return func(u *postgresql.User) {
		u.Metadata = metadata
	}
}

func UserWithVerificationExpiresOn(expiresOn time.Time) MockUserOption {
	return func(u *postgresql.User) {
		u.VerificationExpires = pgtype.Timestamptz{Time: expiresOn, Valid: true}
	}
}

func UserWithVerificationToken(verificationToken string) MockUserOption {
	return func(u *postgresql.User) {
		u.VerificationToken = pgtype.Text{String: verificationToken, Valid: true}
	}
}
func UserWithVerified(verified bool) MockUserOption {
	return func(u *postgresql.User) {
		u.Verified = verified
	}
}

func UserWithCreatedAt(createdAt time.Time) MockUserOption {
	return func(u *postgresql.User) {
		u.CreatedAt = pgtype.Timestamptz{Time: createdAt, Valid: true}
	}
}

func UserWithUpdatedAt(updatedAt time.Time) MockUserOption {
	return func(u *postgresql.User) {
		u.UpdatedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	}
}

func UserWithDeletedAt(deletedAt time.Time) MockUserOption {
	return func(u *postgresql.User) {
		u.DeletedAt = pgtype.Timestamptz{Time: deletedAt, Valid: true}
	}
}

// WithUser maps a mock user to User Model
func WithUser(mockUser identity.User) MockUserOption {
	email := mockUser.Email()
	verification := mockUser.Verification()
	metadata, metadataErr := mockUser.MetadataToBytes()
	if metadataErr != nil {
		metadata = []byte{}
	}
	return func(u *postgresql.User) {
		u.ID = pgtype.UUID{Bytes: mockUser.ID(), Valid: true}
		u.Username = mockUser.Username()
		u.FirstName = pgtype.Text{String: mockUser.FirstName(), Valid: true}
		u.LastName = pgtype.Text{String: mockUser.LastName(), Valid: true}
		u.Email = email.Value()
		u.PasswordHash = mockUser.PasswordHash()
		u.Verified = verification.Verified()
		u.VerificationToken = pgtype.Text{String: verification.Token(), Valid: true}
		u.VerificationExpires = pgtype.Timestamptz{Time: verification.Expires(), Valid: true}
		u.Metadata = metadata
		u.CreatedAt = pgtype.Timestamptz{Time: mockUser.CreatedAt(), Valid: true}
		u.UpdatedAt = pgtype.Timestamptz{Time: mockUser.UpdatedAt(), Valid: true}
		if mockUser.DeletedAt() != nil {
			u.DeletedAt = pgtype.Timestamptz{Time: *mockUser.DeletedAt(), Valid: true}
		}
	}
}
