package identityapp

import (
	"context"
	"errors"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"go.uber.org/mock/gomock"
)

func (suite *IdentityServiceTestSuite) storedUser(status identity.UserStatus, role identity.UserRole) identity.User {
	now := time.Now()
	user, err := identity.NewUser(identity.UserParams{
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
		Status:    status,
		Role:      role,
	})
	suite.Require().NoError(err)
	return *user
}

func (suite *IdentityServiceTestSuite) TestIsAdmin_AnActiveAdminIsAnAdmin() {
	admin := suite.storedUser(identity.UserStatusActive, identity.UserRoleAdmin)
	id := entity.IDToString(admin.ID())
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), id).Return(admin, nil)

	isAdmin, err := suite.service.IsAdmin(context.Background(), id)

	suite.Require().NoError(err)
	suite.True(isAdmin)
}

func (suite *IdentityServiceTestSuite) TestIsAdmin_APlainUserIsNot() {
	user := suite.storedUser(identity.UserStatusActive, identity.UserRoleUser)
	id := entity.IDToString(user.ID())
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), id).Return(user, nil)

	isAdmin, err := suite.service.IsAdmin(context.Background(), id)

	suite.Require().NoError(err)
	suite.False(isAdmin)
}

func (suite *IdentityServiceTestSuite) TestIsAdmin_ASuspendedAdminIsNot() {
	admin := suite.storedUser(identity.UserStatusSuspended, identity.UserRoleAdmin)
	id := entity.IDToString(admin.ID())
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), id).Return(admin, nil)

	isAdmin, err := suite.service.IsAdmin(context.Background(), id)

	suite.Require().NoError(err)
	suite.False(isAdmin)
}

func (suite *IdentityServiceTestSuite) TestIsAdmin_AnUnknownUserIsNot() {
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), "gone").Return(identity.User{}, errdefs.NotFound(errors.New("no rows")))

	isAdmin, err := suite.service.IsAdmin(context.Background(), "gone")

	suite.Require().NoError(err)
	suite.False(isAdmin)
}

func (suite *IdentityServiceTestSuite) TestIsAdmin_ADatastoreFailureIsAnError() {
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), "id").Return(identity.User{}, errors.New("connection refused"))

	isAdmin, err := suite.service.IsAdmin(context.Background(), "id")

	suite.Require().Error(err)
	suite.False(isAdmin)
}
