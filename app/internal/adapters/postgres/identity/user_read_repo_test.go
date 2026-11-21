package identityrepo

import (
	"context"
	"testing"
	"time"

	mockpostgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/curtz/app/pkg/infra/database/mocks"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UserReadRepoAdapterTestSuite struct {
	suite.Suite
	mockCtrl            *gomock.Controller
	mockDbClient        *mockdatabase.MockPostgresDatabaseClient
	mockUserReadQuerier *mockpostgresrepo.MockUserReadQuerier
	mockUserReadRepo    *mockidentity.MockUserReadRepository
	userReadRepoAdapter *userReadRepositoryAdapter
	config              database.Config
}

func (suite *UserReadRepoAdapterTestSuite) SetupTest() {
	config := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	mockCtrl := gomock.NewController(suite.T())
	suite.mockCtrl = mockCtrl
	suite.mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
	suite.mockUserReadQuerier = mockpostgresrepo.NewMockUserReadQuerier(mockCtrl)
	suite.mockUserReadRepo = mockidentity.NewMockUserReadRepository(mockCtrl)
	suite.userReadRepoAdapter = &userReadRepositoryAdapter{
		logPrefix: "UserReadRepoAdapter",
		dbClient:  suite.mockDbClient,
		config:    config,
	}
	suite.config = config

	injectMockUserReadTx(suite.userReadRepoAdapter, suite.mockUserReadQuerier)
}

func TestUserReadRepoAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(UserReadRepoAdapterTestSuite))
}

func (suite *UserReadRepoAdapterTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// TestFetchById_Success tests the FetchById method of the UserReadRepositoryAdapter
func (suite *UserReadRepoAdapterTestSuite) TestFetchById_Success() {
	bcgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bcgCtx, suite.config.OperationTimeout)
	defer cancel()

	userId := entity.NewID()

	mockUser, mockUserErr := mockidentity.MockUser(
		mockidentity.WithId(userId),
	)
	suite.NoError(mockUserErr)

	mockUserRecord := mockpostgresql.MockUser(mockpostgresql.WithUser(*mockUser))
	mockUserStatus := mockpostgresql.MockUserStatus(identity.UserStatusActive)

	existingUser := postgresql.QueryUserByIdRow{
		User:       mockUserRecord,
		UserStatus: mockUserStatus,
	}

	suite.mockUserReadQuerier.
		EXPECT().
		QueryUserById(gomock.Any(), gomock.Any()).
		Return(existingUser, nil).
		Times(1)

	actual, actualErr := suite.userReadRepoAdapter.FetchById(ctx, userId.String())
	suite.Nil(actualErr)
	suite.Equal(mockUser.ID(), actual.ID())
	suite.Equal(mockUser.Username(), actual.Username())
	suite.Equal(mockUser.Email(), actual.Email())
	suite.Equal(mockUser.FirstName(), actual.FirstName())
	suite.Equal(mockUser.LastName(), actual.LastName())
	suite.Equal(mockUser.CreatedAt(), actual.CreatedAt())
	suite.Equal(mockUser.UpdatedAt(), actual.UpdatedAt())
}
