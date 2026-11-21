package identitydatastore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/onsi/ginkgo/v2"
	mockpostgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/mocks"
	postgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql"
	mockpostgresql "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/sql/mocks"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	mockdatabase "github.com/sanctumlabs/curtz/app/pkg/infra/database/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var _ = ginkgo.Describe("User Write Datastore Adapter Unit Test Suite", ginkgo.Ordered, func() {
	ctx := context.Background()
	var (
		mockCtrl             *gomock.Controller
		mockDbClient         *mockdatabase.MockPostgresDatabaseClient
		mockUserWriteQuerier *mockpostgresrepo.MockUserWriteQuerier
		userWriteDatastore   *userWriteDatastoreAdapter
	)

	ginkgo.BeforeAll(func() {
		config := database.Config{
			OperationTimeout: 30 * time.Second,
			RetryConfig:      recoveryutils.DefaultRetryConfig,
		}
		mockCtrl = gomock.NewController(ginkgo.GinkgoT())
		mockDbClient = mockdatabase.NewMockPostgresDatabaseClient(mockCtrl)
		mockUserWriteQuerier = mockpostgresrepo.NewMockUserWriteQuerier(mockCtrl)
		userWriteDatastore = &userWriteDatastoreAdapter{
			logPrefix: "UserWriteDatastoreAdapter",
			dbClient:  mockDbClient,
			config:    config,
		}

		injectMockUserWriteTx(userWriteDatastore, mockUserWriteQuerier)
	})

	ginkgo.AfterEach(func() {
		mockCtrl.Finish()
	})

	mockUserId := entity.NewID()
	mockUser, mockUserErr := mockidentity.MockUser(
		mockidentity.WithId(mockUserId),
	)
	assert.NoError(ginkgo.GinkgoT(), mockUserErr)

	mockUserRecord := mockpostgresql.MockUser(mockpostgresql.WithUser(*mockUser))
	mockWUserQueryByIdRow := postgresql.QueryUserByIdRow{User: mockUserRecord}

	ginkgo.Describe("Save", func() {
		ginkgo.It("saves a new user successfully", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryCreateUser(gomock.Any(), gomock.Any()).
				Return(mockUserRecord, nil).
				Times(1)

			actual, actualErr := userWriteDatastore.Save(ctx, *mockUser)
			assert.Nil(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), mockUser.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Username(), actual.Username())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Email(), actual.Email())
			assert.Equal(ginkgo.GinkgoT(), mockUser.FirstName(), actual.FirstName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.LastName(), actual.LastName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.CreatedAt(), actual.CreatedAt())
			assert.Equal(ginkgo.GinkgoT(), mockUser.UpdatedAt(), actual.UpdatedAt())
		})
	})

	ginkgo.Describe("Outbox", func() {
		registerUser := func() *identity.User {
			registered, registerErr := identity.Register(identity.RegisterUserParams{
				Username:  "johndoe",
				FirstName: "John",
				Email:     "john.doe@curtz.com",
			})
			assert.NoError(ginkgo.GinkgoT(), registerErr)
			return registered
		}

		ginkgo.It("writes each recorded event to the outbox in the save transaction", func() {
			registered := registerUser()
			event := registered.DomainEvents()[0]
			userID := entity.IDToString(registered.ID())

			mockUserWriteQuerier.EXPECT().QueryCreateUser(gomock.Any(), gomock.Any()).Return(mockUserRecord, nil).Times(1)
			mockUserWriteQuerier.
				EXPECT().
				QueryCreateOutboxEvent(gomock.Any(), gomock.Cond(func(params postgresql.QueryCreateOutboxEventParams) bool {
					eventID, _ := postgres.UUIDToString(params.ID)
					return eventID == event.ID() &&
						params.EventType == "user.registered" &&
						params.Destination == "identity.events" &&
						params.PartitionKey.String == userID &&
						len(params.Payload) > 0 && len(params.Headers) > 0
				})).
				Return(postgresql.OutboxEvent{}, nil).
				Times(1)

			_, actualErr := userWriteDatastore.Save(ctx, *registered)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
		})

		ginkgo.It("fails the save when the event cannot be written, so the user is not committed without it", func() {
			registered := registerUser()
			outboxErr := errors.New("outbox insert failed")

			mockUserWriteQuerier.EXPECT().QueryCreateUser(gomock.Any(), gomock.Any()).Return(mockUserRecord, nil).Times(1)
			mockUserWriteQuerier.EXPECT().QueryCreateOutboxEvent(gomock.Any(), gomock.Any()).Return(postgresql.OutboxEvent{}, outboxErr).Times(1)

			actual, actualErr := userWriteDatastore.Save(ctx, *registered)
			assert.ErrorIs(ginkgo.GinkgoT(), actualErr, outboxErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})

		ginkgo.It("persists a verification with its status and UserVerified event", func() {
			// as loaded from the datastore: inactive, unverified, and carrying no events yet
			loaded, loadedErr := mockidentity.MockUser(
				mockidentity.WithStatus(identity.UserStatusInactive),
				mockidentity.WithVerified(false),
				mockidentity.WithVerificationToken("a-verification-token"),
				mockidentity.WithVerificationExpires(time.Now().Add(time.Hour)),
			)
			assert.NoError(ginkgo.GinkgoT(), loadedErr)
			assert.NoError(ginkgo.GinkgoT(), loaded.Verify("a-verification-token", time.Now()))

			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserVerification(gomock.Any(), gomock.Cond(func(params postgresql.QueryUpdateUserVerificationParams) bool {
					return params.Verified
				})).
				Return(mockUserRecord, nil).
				Times(1)
			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserStatus(gomock.Any(), gomock.Cond(func(params postgresql.QueryUpdateUserStatusParams) bool {
					return params.Status == postgresql.UserStatusACTIVE
				})).
				Return(mockUserRecord, nil).
				Times(1)
			var eventTypes []string
			mockUserWriteQuerier.
				EXPECT().
				QueryCreateOutboxEvent(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, params postgresql.QueryCreateOutboxEventParams) (postgresql.OutboxEvent, error) {
					eventTypes = append(eventTypes, params.EventType)
					return postgresql.OutboxEvent{}, nil
				}).
				Times(1)

			_, actualErr := userWriteDatastore.MarkVerified(ctx, *loaded)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), []string{"user.verified"}, eventTypes)
		})
	})

	ginkgo.Describe("Update", func() {
		ginkgo.It("successfully updates a user and returns the updated user", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserDetails(gomock.Any(), gomock.Any()).
				Return(mockUserRecord, nil).
				Times(1)

			actual, actualErr := userWriteDatastore.Update(ctx, *mockUser)
			assert.Nil(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), mockUser.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Username(), actual.Username())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Email(), actual.Email())
			assert.Equal(ginkgo.GinkgoT(), mockUser.FirstName(), actual.FirstName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.LastName(), actual.LastName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.CreatedAt(), actual.CreatedAt())
			assert.Equal(ginkgo.GinkgoT(), mockUser.UpdatedAt(), actual.UpdatedAt())
		})

		ginkgo.It("persists the status the aggregate transitioned to", func() {
			deletedUser, deletedUserErr := mockidentity.MockUser(mockidentity.WithStatus(identity.UserStatusDeleted))
			assert.NoError(ginkgo.GinkgoT(), deletedUserErr)

			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserDetails(gomock.Any(), gomock.Cond(func(params postgresql.QueryUpdateUserDetailsParams) bool {
					return params.Status == postgresql.UserStatusDELETED
				})).
				Return(mockpostgresql.MockUser(mockpostgresql.WithUser(*deletedUser)), nil).
				Times(1)

			actual, actualErr := userWriteDatastore.Update(ctx, *deletedUser)
			assert.Nil(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), identity.UserStatusDeleted, actual.Status())
		})

		ginkgo.It("returns error when there is a failure to update a user", func() {
			updateUserDetailsErr := errors.New("Failed to update user")
			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserDetails(gomock.Any(), gomock.Any()).
				Return(postgresql.User{}, updateUserDetailsErr).
				Times(1)

			actual, actualErr := userWriteDatastore.Update(ctx, *mockUser)
			assert.NotNil(ginkgo.GinkgoT(), actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})
	})

	ginkgo.Describe("UpdateMetadata", func() {
		updateMetadataRequest := identity.UpdateUserMetadataVerificationRequest{
			ID: mockUserId.String(),
			Metadata: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
				"key3": true,
			},
		}

		ginkgo.It("successfully updates user metadata returns the updated user", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUserById(gomock.Any(), gomock.Any()).
				Return(mockWUserQueryByIdRow, nil).
				Times(1)

			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserMetadata(gomock.Any(), gomock.Any()).
				Return(mockUserRecord, nil).
				Times(1)

			actual, actualErr := userWriteDatastore.UpdateMetadata(ctx, updateMetadataRequest)
			assert.Nil(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), mockUser.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Username(), actual.Username())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Email(), actual.Email())
			assert.Equal(ginkgo.GinkgoT(), mockUser.FirstName(), actual.FirstName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.LastName(), actual.LastName())
			assert.Equal(ginkgo.GinkgoT(), mockUser.CreatedAt(), actual.CreatedAt())
			assert.Equal(ginkgo.GinkgoT(), mockUser.UpdatedAt(), actual.UpdatedAt())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Verification(), actual.Verification())
			assert.Equal(ginkgo.GinkgoT(), mockUser.Metadata(), actual.Metadata())
		})

		ginkgo.It("returns error when there is a failure to update user metadata", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUserById(gomock.Any(), gomock.Any()).
				Return(mockWUserQueryByIdRow, nil).
				Times(1)

			updateErr := errors.New("failed to update user metadata")
			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserMetadata(gomock.Any(), gomock.Any()).
				Return(postgresql.User{}, updateErr).
				Times(1)

			actual, actualErr := userWriteDatastore.UpdateMetadata(ctx, updateMetadataRequest)
			assert.NotNil(ginkgo.GinkgoT(), actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})
	})
	ginkgo.Describe("UpdatePassword", func() {
		updatePasswordRequest := identity.UpdateUserPasswordRequest{
			ID:           mockUserId.String(),
			PasswordHash: "new-hash",
		}

		ginkgo.It("successfully updates the password and returns the updated user", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUserById(gomock.Any(), gomock.Any()).
				Return(mockWUserQueryByIdRow, nil).
				Times(1)

			updatedRecord := mockpostgresql.MockUser(
				mockpostgresql.WithUser(*mockUser),
				mockpostgresql.UserWithPasswordHash("new-hash"),
			)
			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserPassword(gomock.Any(), postgresql.QueryUpdateUserPasswordParams{
					ID:           mockUserRecord.ID,
					PasswordHash: "new-hash",
				}).
				Return(updatedRecord, nil).
				Times(1)

			actual, actualErr := userWriteDatastore.UpdatePassword(ctx, updatePasswordRequest)
			assert.Nil(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), mockUser.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), "new-hash", actual.PasswordHash())
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUserById(gomock.Any(), gomock.Any()).
				Return(postgresql.QueryUserByIdRow{}, pgx.ErrNoRows).
				Times(1)

			actual, actualErr := userWriteDatastore.UpdatePassword(ctx, updatePasswordRequest)
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})

		ginkgo.It("returns error when there is a failure to update the password", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryUserById(gomock.Any(), gomock.Any()).
				Return(mockWUserQueryByIdRow, nil).
				Times(1)

			mockUserWriteQuerier.
				EXPECT().
				QueryUpdateUserPassword(gomock.Any(), gomock.Any()).
				Return(postgresql.User{}, errors.New("failed to update password")).
				Times(1)

			actual, actualErr := userWriteDatastore.UpdatePassword(ctx, updatePasswordRequest)
			assert.NotNil(ginkgo.GinkgoT(), actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})
	})

	ginkgo.Describe("SoftDelete", func() {
		ginkgo.It("marks the user as deleted", func() {
			mockUserWriteQuerier.
				EXPECT().
				QuerySoftDeleteUser(gomock.Any(), gomock.Cond(func(p postgresql.QuerySoftDeleteUserParams) bool {
					return p.ID == mockUserRecord.ID && p.DeletedAt.Valid && !p.DeletedAt.Time.IsZero()
				})).
				Return(mockUserRecord, nil).
				Times(1)

			actualErr := userWriteDatastore.SoftDelete(ctx, mockUserId.String())
			assert.Nil(ginkgo.GinkgoT(), actualErr)
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			mockUserWriteQuerier.
				EXPECT().
				QuerySoftDeleteUser(gomock.Any(), gomock.Any()).
				Return(postgresql.User{}, pgx.ErrNoRows).
				Times(1)

			actualErr := userWriteDatastore.SoftDelete(ctx, mockUserId.String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})

		ginkgo.It("returns error for an invalid id without hitting the database", func() {
			actualErr := userWriteDatastore.SoftDelete(ctx, "not-a-uuid")
			assert.NotNil(ginkgo.GinkgoT(), actualErr)
		})
	})

	ginkgo.Describe("Delete", func() {
		ginkgo.It("permanently deletes the user", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryDeleteUserWithId(gomock.Any(), mockUserRecord.ID).
				Return(mockUserRecord, nil).
				Times(1)

			actualErr := userWriteDatastore.Delete(ctx, mockUserId.String())
			assert.Nil(ginkgo.GinkgoT(), actualErr)
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			mockUserWriteQuerier.
				EXPECT().
				QueryDeleteUserWithId(gomock.Any(), gomock.Any()).
				Return(postgresql.User{}, pgx.ErrNoRows).
				Times(1)

			actualErr := userWriteDatastore.Delete(ctx, mockUserId.String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})
})
