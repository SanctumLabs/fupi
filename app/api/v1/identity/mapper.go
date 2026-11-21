package identityapi

import (
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
)

// toUserResponse maps a User aggregate onto the wire representation. The password hash and the
// verification token are deliberately absent: neither is ever returned to a client.
func toUserResponse(user identity.User) userResponseDto {
	email := user.Email()
	verification := user.Verification()

	return userResponseDto{
		ID:        entity.IDToString(user.ID()),
		Username:  user.Username(),
		FirstName: user.FirstName(),
		LastName:  user.LastName(),
		Email:     email.Value(),
		Status:    string(user.Status()),
		Verified:  verification.Verified(),
		CreatedAt: user.CreatedAt(),
		UpdatedAt: user.UpdatedAt(),
	}
}
