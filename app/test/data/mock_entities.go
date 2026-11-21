//go:build legacy

package data

import (
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
)

// MockUser creates a mock user given an email and a password
func MockUser(email, password string) (entities.User, error) {
	user, err := entities.NewUser(email, password)
	return user, err
}
