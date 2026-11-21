//go:build legacy

package entities

import (
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

const (
	// USER_STATUS_ACTIVE UserStatusActive is the status for an active user
	USER_STATUS_ACTIVE      = "ACTIVE"
	USER_STATUS_DEACTIVATED = "DEACTIVATED"
)

// User is a domain entity for user
type User struct {
	// ID is the unique identifier for a user
	identifier.ID

	// BaseEntity is the base entity for a user
	BaseEntity

	// Email is the email address for a user
	*Email

	// Password is the password for a user
	Password

	// Token is the token for a user
	Token

	// Verified is the verification status for a user
	Verified bool

	// Status is the status for a user
	Status string
}

func NewUser(email, password string) (User, error) {
	userPassword, err := NewPassword(password)
	if err != nil {
		return User{}, err
	}

	userEmail, err := NewEmail(email)
	if err != nil {
		return User{}, err
	}

	id := identifier.New()
	userToken := NewToken()
	now := time.Now()
	baseModel := NewBaseEntity(BaseEntityParams{
		ID:        id,
		Metadata:  nil,
		CreatedAt: now,
		UpdatedAt: now,
		DeletedAt: nil,
	})

	return User{
		ID:         id,
		Email:      userEmail,
		Password:   userPassword,
		Token:      userToken,
		BaseEntity: baseModel,
	}, nil
}
