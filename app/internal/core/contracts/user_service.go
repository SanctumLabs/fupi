//go:build legacy

package contracts

import (
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

type UserService interface {
	UserReadService
	UserWriteService
}

type UserReadService interface {
	GetUserByEmail(email string) (entities.User, error)
	GetUserByID(id string) (entities.User, error)
	GetByVerificationToken(verificationToken string) (entities.User, error)
}

type UserWriteService interface {
	CreateUser(email, password string) (entities.User, error)
	SetVerified(id identifier.ID) error
	RemoveUser(id string) error
}
