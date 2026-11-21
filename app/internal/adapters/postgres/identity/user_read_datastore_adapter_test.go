package identitydatastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	mockpostgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/curtz/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UserReadDatastoreAdapterTestSuite struct {
	suite.Suite
	mockCtrl                 *gomock.Controller
	mockDbClient             *mockdatabase.MockPostgresDatabaseClient
	mockUserReadQuerier      *mockpostgresrepo.MockUserReadQuerier
	mockUserReadDatastore    *mockidentity.MockUserReadDatastore
	userReadDatastoreAdapter *userReadDatastoreAdapter
	config                   database.Config
}

func (suite *UserReadDatastoreAdapterTestSuite) SetupTest() {
	config := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	mockCtrl := gomock.NewController(suite.T())
	suite.mockCtrl = mockCtrl
	suite.mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
	suite.mockUserReadQuerier = mockpostgresrepo.NewMockUserReadQuerier(mockCtrl)
	suite.mockUserReadDatastore = mockidentity.NewMockUserReadDatastore(mockCtrl)
	suite.userReadDatastoreAdapter = &userReadDatastoreAdapter{
		logPrefix: "UserReadDatastoreAdapter",
		dbClient:  suite.mockDbClient,
		config:    config,
	}
	suite.config = config

	injectMockUserReadTx(suite.userReadDatastoreAdapter, suite.mockUserReadQuerier)
}

func TestUserReadDatastoreAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(UserReadDatastoreAdapterTestSuite))
}

func (suite *UserReadDatastoreAdapterTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// TestFetchById_Success tests the FetchById method of the UserReadRepositoryAdapter
func (suite *UserReadDatastoreAdapterTestSuite) TestFetchById_Success() {
	bcgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bcgCtx, suite.config.OperationTimeout)
	defer cancel()

	userId := entity.NewID()

	mockUser, mockUserErr := mockidentity.MockUser(
		mockidentity.WithId(userId),
	)
	suite.NoError(mockUserErr)

	mockUserRecord := mockpostgresql.MockUser(mockpostgresql.WithUser(*mockUser))
	existingUser := postgresql.QueryUserByIdRow{User: mockUserRecord}

	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserById(gomock.Any(), gomock.Any()).
		Return(existingUser, nil).
		Times(1)

	actual, actualErr := suite.userReadDatastoreAdapter.FetchById(ctx, userId.String())
	suite.Nil(actualErr)
	suite.Equal(mockUser.ID(), actual.ID())
	suite.Equal(mockUser.Username(), actual.Username())
	suite.Equal(mockUser.Email(), actual.Email())
	suite.Equal(mockUser.FirstName(), actual.FirstName())
	suite.Equal(mockUser.LastName(), actual.LastName())
	suite.Equal(mockUser.CreatedAt(), actual.CreatedAt())
	suite.Equal(mockUser.UpdatedAt(), actual.UpdatedAt())
}

func (suite *UserReadDatastoreAdapterTestSuite) newMockUserRow() (*identity.User, postgresql.User) {
	mockUser, mockUserErr := mockidentity.MockUser(mockidentity.WithId(entity.NewID()))
	suite.Require().NoError(mockUserErr)
	return mockUser, mockpostgresql.MockUser(mockpostgresql.WithUser(*mockUser))
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByUsername_Success() {
	ctx := context.Background()
	mockUser, userRecord := suite.newMockUserRow()

	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByUsername(gomock.Any(), mockUser.Username()).
		Return(postgresql.QueryUserByUsernameRow{User: userRecord}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchByUsername(ctx, mockUser.Username())
	suite.NoError(err)
	suite.Equal(mockUser.ID(), actual.ID())
	suite.Equal(mockUser.Username(), actual.Username())
	suite.Equal(identity.UserStatusActive, actual.Status())
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByUsername_NotFound() {
	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByUsername(gomock.Any(), "ghost").
		Return(postgresql.QueryUserByUsernameRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.userReadDatastoreAdapter.FetchByUsername(context.Background(), "ghost")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByEmail_Success() {
	ctx := context.Background()
	mockUser, userRecord := suite.newMockUserRow()
	email := mockUser.Email()

	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByEmail(gomock.Any(), email.Value()).
		Return(postgresql.QueryUserByEmailRow{User: userRecord}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchByEmail(ctx, email.Value())
	suite.NoError(err)
	suite.Equal(mockUser.ID(), actual.ID())
	suite.Equal(email, actual.Email())
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByEmail_NotFound() {
	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByEmail(gomock.Any(), "ghost@example.com").
		Return(postgresql.QueryUserByEmailRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.userReadDatastoreAdapter.FetchByEmail(context.Background(), "ghost@example.com")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchAll_Success() {
	ctx := context.Background()
	first, firstRecord := suite.newMockUserRow()
	second, secondRecord := suite.newMockUserRow()

	params := common.NewRequestParams(common.WithRequestLimit(2), common.WithOffset(2))

	suite.mockUserReadQuerier.
		EXPECT().
		QueryAllUsers(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUsersParams) bool {
			return p.LimitBy == 2 && p.CurrentOffset == 2 && !p.UserStatus.Valid && !p.IncludeDeleted &&
				p.OrderBy == string(common.OrderByCreatedAt) && p.SortOrder == string(common.SortOrderDesc)
		})).
		Return([]postgresql.QueryAllUsersRow{
			{User: firstRecord, TotalRecords: 5},
			{User: secondRecord, TotalRecords: 5},
		}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchAll(ctx, params)
	suite.NoError(err)
	suite.Equal(5, actual.Total)
	suite.Equal(2, actual.Size)
	suite.Equal(2, actual.Page)
	suite.Require().Len(actual.Records, 2)
	suite.Equal(first.ID(), actual.Records[0].ID())
	suite.Equal(second.ID(), actual.Records[1].ID())
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchAll_QueryError() {
	suite.mockUserReadQuerier.
		EXPECT().
		QueryAllUsers(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("boom")).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchAll(context.Background(), common.NewRequestParams())
	suite.Error(err)
	suite.Empty(actual.Records)
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByStatus_FiltersByStatus() {
	ctx := context.Background()
	mockUser, userRecord := suite.newMockUserRow()
	userRecord.Status = postgresql.UserStatusSUSPENDED

	suite.mockUserReadQuerier.
		EXPECT().
		QueryAllUsers(gomock.Any(), gomock.Cond(func(p postgresql.QueryAllUsersParams) bool {
			return p.UserStatus.Valid && p.UserStatus.UserStatus == postgresql.UserStatusSUSPENDED
		})).
		Return([]postgresql.QueryAllUsersRow{{User: userRecord, TotalRecords: 1}}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchByStatus(ctx, identity.UserStatusSuspended)
	suite.NoError(err)
	suite.Equal(1, actual.Total)
	suite.Require().Len(actual.Records, 1)
	suite.Equal(mockUser.ID(), actual.Records[0].ID())
	suite.Equal(identity.UserStatusSuspended, actual.Records[0].Status())
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByStatus_Empty() {
	suite.mockUserReadQuerier.
		EXPECT().
		QueryAllUsers(gomock.Any(), gomock.Any()).
		Return([]postgresql.QueryAllUsersRow{}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchByStatus(context.Background(), identity.UserStatusDeleted)
	suite.NoError(err)
	suite.Equal(0, actual.Total)
	suite.Empty(actual.Records)
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByVerificationToken_Success() {
	ctx := context.Background()
	mockUser, userRecord := suite.newMockUserRow()
	verification := mockUser.Verification()
	token := verification.Token()

	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByVerificationToken(gomock.Any(), pgtype.Text{String: token, Valid: true}).
		Return(postgresql.QueryUserByVerificationTokenRow{User: userRecord}, nil).
		Times(1)

	actual, err := suite.userReadDatastoreAdapter.FetchByVerificationToken(ctx, token)
	suite.NoError(err)
	suite.Equal(mockUser.ID(), actual.ID())

	actualVerification := actual.Verification()
	suite.Equal(token, actualVerification.Token())
}

func (suite *UserReadDatastoreAdapterTestSuite) TestFetchByVerificationToken_NotFound() {
	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserByVerificationToken(gomock.Any(), gomock.Any()).
		Return(postgresql.QueryUserByVerificationTokenRow{}, pgx.ErrNoRows).
		Times(1)

	_, err := suite.userReadDatastoreAdapter.FetchByVerificationToken(context.Background(), "no-such-token")
	suite.Error(err)
	suite.True(errdefs.IsNotFound(err), "expected a NotFound error, got %v", err)
}
