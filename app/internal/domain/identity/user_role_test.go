package identity

import (
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
)

func hydrate(t *testing.T, role UserRole) *User {
	t.Helper()
	now := time.Now()
	user, err := NewUser(UserParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams:        entity.EntityIDParams{ID: entity.NewID()},
				EntityTimestampParams: entity.EntityTimestampParams{CreatedAt: now, UpdatedAt: now},
			},
		},
		Username:  "johndoe",
		FirstName: "John",
		LastName:  "Doe",
		Email:     "john.doe@fupi.com",
		Status:    UserStatusActive,
		Role:      role,
	})
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	return user
}

func TestUserRole(t *testing.T) {
	t.Run("a registered user is not an admin", func(t *testing.T) {
		user := mustRegister(t)

		if user.Role() != UserRoleUser {
			t.Errorf("role = %s, want %s", user.Role(), UserRoleUser)
		}
		if user.IsAdmin() {
			t.Error("a freshly registered user must not be an admin")
		}
	})

	t.Run("a hydrated user without a role is a plain user", func(t *testing.T) {
		user := hydrate(t, "")

		if user.Role() != UserRoleUser || user.IsAdmin() {
			t.Errorf("role = %s, want %s", user.Role(), UserRoleUser)
		}
	})

	t.Run("a hydrated admin is an admin", func(t *testing.T) {
		if !hydrate(t, UserRoleAdmin).IsAdmin() {
			t.Error("expected an admin")
		}
	})
}
