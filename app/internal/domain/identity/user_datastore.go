package identity

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/core/ports/repository"
)

type (

	// UserDatastore defines the interface for interacting with User entities in an underlying storage implementation
	UserDatastore interface {
		UserReadDatastore
		UserWriteDatastore
	}

	// UserWriteDatastore defines the interface for writing User entities to an underlying storage implementation
	UserWriteDatastore interface {
		repository.WriteRepositoryPort[User]

		// MarkVerified persists a User that has just completed email verification (see User.Verify):
		// its verified flag, status and recorded events are written atomically
		MarkVerified(ctx context.Context, user User) (User, error)

		// UpdateMetadata updates the metadata of a User entity based on the provided request and returns the updated User entity
		UpdateMetadata(ctx context.Context, request UpdateUserMetadataVerificationRequest) (User, error)

		// UpdatePassword updates the password of a User entity based on the provided request and returns the updated User entity
		UpdatePassword(ctx context.Context, request UpdateUserPasswordRequest) (User, error)
	}

	// UserReadDatastore defines the interface for reading User entities from an underlying storage implementation
	UserReadDatastore interface {
		repository.ReadRepositoryPort[User]

		// FetchByUsername retrieves a User entity by its username
		FetchByUsername(ctx context.Context, username string) (User, error)

		// FetchByEmail retrieves a User entity by its email
		FetchByEmail(ctx context.Context, email string) (User, error)

		// FetchByVerificationToken retrieves a User entity by its email-verification token
		FetchByVerificationToken(ctx context.Context, token string) (User, error)

		// FetchByStatus retrieves a list of User entities by their status
		FetchByStatus(ctx context.Context, status UserStatus) (repository.FetchRecordsResponse[User], error)
	}
)
