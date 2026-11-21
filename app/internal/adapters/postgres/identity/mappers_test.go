package identitydatastore

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapUserModelToEntity_CarriesTheRole(t *testing.T) {
	for role, want := range map[postgresql.UserRole]identity.UserRole{
		postgresql.UserRoleUser:  identity.UserRoleUser,
		postgresql.UserRoleAdmin: identity.UserRoleAdmin,
	} {
		model := postgresql.User{
			ID:        pgtype.UUID{Bytes: entity.NewID(), Valid: true},
			Username:  "johndoe",
			FirstName: pgtype.Text{String: "John", Valid: true},
			Email:     "john.doe@fupi.com",
			Status:    postgresql.UserStatusACTIVE,
			Role:      role,
		}

		user, err := MapUserModelToEntity(model)

		require.NoError(t, err)
		assert.Equal(t, want, user.Role())
		assert.Equal(t, want == identity.UserRoleAdmin, user.IsAdmin())
	}
}
