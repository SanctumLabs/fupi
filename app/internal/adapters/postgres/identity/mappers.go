package identityrepo

import (
	"fmt"
	"log/slog"
	"time"

	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
)

// UserMapperParams holds the parameters for mapping a postgresql.User model to an identity.User entity.
type UserMapperParams struct {
	UserModel postgresql.User
	Status    string
}

// MapUserModelToEntity maps a postgresql.User model to an identity.User entity.
func MapUserModelToEntity(params UserMapperParams) (identity.User, error) {
	userModel := params.UserModel
	userId, userIdErr := postgres.UUIDToString(userModel.ID)
	if userIdErr != nil {
		return identity.User{}, fmt.Errorf("failed to parse id when mapping user %v with error %w", userModel.ID, userIdErr)
	}
	userEntityId, userEntityIdErr := entity.StringToID(userId)
	if userEntityIdErr != nil {
		return identity.User{}, fmt.Errorf("failed to parse user entity id when mapping user %v with error %w", userModel.ID, userEntityIdErr)
	}

	// Build optional timestamps only when valid
	var deletedAt *time.Time
	if userModel.DeletedAt.Valid {
		t := userModel.DeletedAt.Time
		deletedAt = &t
	}

	metadata := map[string]any{}
	if userModel.Metadata != nil {
		userMetadata, metadataErr := entity.BytesToMetadata(userModel.Metadata)
		if metadataErr != nil {
			// Log a warning for failing to parse metadata, instead of failing
			slog.Warn("failed to parse metadata when mapping user, skipping metadata mapping", "metadata", userModel.Metadata, "error", metadataErr)
		}
		metadata = userMetadata
	}

	userParams := identity.UserParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID: userEntityId,
				},
				EntityTimestampParams: entity.EntityTimestampParams{
					CreatedAt: userModel.CreatedAt.Time,
					UpdatedAt: userModel.UpdatedAt.Time,
					DeletedAt: deletedAt,
				},
				Metadata: metadata,
			},
		},
		Username:            userModel.Username,
		FirstName:           userModel.FirstName.String,
		LastName:            userModel.LastName.String,
		Email:               userModel.Email,
		Status:              identity.UserStatus(params.Status),
		VerificationToken:   userModel.VerificationToken.String,
		VerificationExpires: userModel.VerificationExpires.Time,
		Verified:            userModel.Verified,
	}

	userEntity, userEntityErr := identity.NewUser(userParams)
	if userEntityErr != nil {
		return identity.User{}, fmt.Errorf("failed to create user entity from model %v with error %w", params, userEntityErr)
	}

	return *userEntity, nil
}
