//go:build integration

package identitydatastore

import (
	"context"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/onsi/ginkgo/v2"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/fupi/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
)

var _ = ginkgo.Describe("User Write Datastore Adapter Integration Test Suite", ginkgo.Ordered, func() {
	ctx := context.Background()

	var (
		testPostgresDatabaseClient database.PostgresDatabaseClient
		userWriteDatastoreAdapter  identity.UserWriteDatastore
	)

	ginkgo.BeforeAll(func() {
		var err error
		testPostgresDatabaseClient, err = test.TestPostgresDatabaseClient(ctx)
		if err != nil {
			assert.FailNow(ginkgo.GinkgoT(), "failed to create database client: %s", err.Error())
		}

		config := database.Config{
			OperationTimeout: 5 * time.Minute,
			RetryConfig:      recoveryutils.DefaultRetryConfig,
		}

		userWriteDatastoreAdapter = NewUserWriteDatastoreAdapter(
			testPostgresDatabaseClient,
			config,
		)
	})

	ginkgo.AfterAll(func() {
		testPostgresDatabaseClient.Close()
	})

	ginkgo.Describe("Save", func() {
		ginkgo.It("saves a new user successfully", func() {
			mockUser, mockUserErr := mockidentity.MockUser()
			assert.NoError(ginkgo.GinkgoT(), mockUserErr)

			// Require stops the test immediately on failure, preventing a nil
			// dereference on *mockUser in the Create call below.
			assert.NoError(ginkgo.GinkgoT(), mockUserErr)

			actual, actualErr := userWriteDatastoreAdapter.Save(ctx, *mockUser)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.NotNil(ginkgo.GinkgoT(), actual)
		})
	})

	ginkgo.Describe("Outbox", func() {
		ginkgo.It("commits a registered user's UserRegistered event with the user", func() {
			registered, registerErr := identity.Register(identity.RegisterUserParams{
				Username:  faker.Username(),
				FirstName: "John",
				Email:     faker.Email(),
			})
			assert.NoError(ginkgo.GinkgoT(), registerErr)
			event := registered.DomainEvents()[0]

			_, saveErr := userWriteDatastoreAdapter.Save(ctx, *registered)
			assert.NoError(ginkgo.GinkgoT(), saveErr)

			eventID, idErr := postgres.StringToUUID(event.ID())
			assert.NoError(ginkgo.GinkgoT(), idErr)
			outboxEvent, outboxErr := postgresql.New(testPostgresDatabaseClient.GetDB()).QueryOutboxEventById(ctx, eventID)
			assert.NoError(ginkgo.GinkgoT(), outboxErr)
			assert.Equal(ginkgo.GinkgoT(), "user.registered", outboxEvent.OutboxEvent.EventType)
			assert.Equal(ginkgo.GinkgoT(), "identity.events", outboxEvent.OutboxEvent.Destination)
			assert.Equal(ginkgo.GinkgoT(), entity.IDToString(registered.ID()), outboxEvent.OutboxEvent.PartitionKey.String)
			assert.False(ginkgo.GinkgoT(), outboxEvent.OutboxEvent.SentTime.Valid, "a new event is not yet sent")
		})
	})

	ginkgo.Describe("Update", func() {
		mockUser, mockUserErr := mockidentity.MockUser()
		assert.NoError(ginkgo.GinkgoT(), mockUserErr)

		ginkgo.It("updates an existing user successfully", func() {
			actualCreatedUser, createError := userWriteDatastoreAdapter.Save(ctx, *mockUser)
			assert.NoError(ginkgo.GinkgoT(), createError)
			assert.NotNil(ginkgo.GinkgoT(), actualCreatedUser)

			updatedEmail, updatedEmailErr := identity.NewEmail(faker.Email())
			assert.NoError(ginkgo.GinkgoT(), updatedEmailErr)

			newUser := actualCreatedUser.WithEmail(updatedEmail)

			actual, actualErr := userWriteDatastoreAdapter.Update(ctx, newUser)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.NotNil(ginkgo.GinkgoT(), actual)

			actualEmail := actual.Email()
			assert.Equal(ginkgo.GinkgoT(), updatedEmail.Value(), actualEmail.Value())
		})

		ginkgo.It("returns error if there is a failure to update a user", func() {
			mockUser, mockUserErr := mockidentity.MockUser()
			assert.NoError(ginkgo.GinkgoT(), mockUserErr)

			actual, actualErr := userWriteDatastoreAdapter.Update(ctx, *mockUser)
			assert.Error(ginkgo.GinkgoT(), actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual)
		})

		ginkgo.Describe("Metadata", func() {
			anotherMockUser, anotherMockUserErr := mockidentity.MockUser(
				mockidentity.WithUsername(faker.Username()),
				mockidentity.WithEmail(faker.Email()),
			)
			assert.NoError(ginkgo.GinkgoT(), anotherMockUserErr)

			ginkgo.It("updates an existing user metadata successfully", func() {
				actualAnotherCreatedUser, anotherCreatedError := userWriteDatastoreAdapter.Save(ctx, *anotherMockUser)
				assert.NoError(ginkgo.GinkgoT(), anotherCreatedError)
				assert.NotNil(ginkgo.GinkgoT(), actualAnotherCreatedUser)

				updateMetadataRequest := identity.UpdateUserMetadataVerificationRequest{
					ID: actualAnotherCreatedUser.ID().String(),
					Metadata: map[string]any{
						"key1": "value1",
						"key2": 42,
						"key3": true,
					},
				}

				actual, actualErr := userWriteDatastoreAdapter.UpdateMetadata(ctx, updateMetadataRequest)
				assert.NoError(ginkgo.GinkgoT(), actualErr)
				assert.NotNil(ginkgo.GinkgoT(), actual)

				actualMetadata := actual.Metadata()
				// metadata round-trips through JSONB, so numbers come back as float64
				assert.EqualValues(ginkgo.GinkgoT(), updateMetadataRequest.Metadata["key1"], actualMetadata["key1"])
				assert.EqualValues(ginkgo.GinkgoT(), updateMetadataRequest.Metadata["key2"], actualMetadata["key2"])
				assert.EqualValues(ginkgo.GinkgoT(), updateMetadataRequest.Metadata["key3"], actualMetadata["key3"])
			})

			ginkgo.It("returns not found when the user does not exist", func() {
				actual, actualErr := userWriteDatastoreAdapter.UpdateMetadata(ctx, identity.UpdateUserMetadataVerificationRequest{
					ID:       entity.NewID().String(),
					Metadata: map[string]any{"key": "value"},
				})
				assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
				assert.Empty(ginkgo.GinkgoT(), actual)
			})
		})
	})

	// createUser persists a fresh user with a unique username/email and fails the spec if that fails.
	createUser := func() identity.User {
		mockUser, mockUserErr := mockidentity.MockUser(
			mockidentity.WithUsername(faker.Username()),
			mockidentity.WithEmail(faker.Email()),
		)
		assert.NoError(ginkgo.GinkgoT(), mockUserErr)

		created, createErr := userWriteDatastoreAdapter.Save(ctx, *mockUser)
		if createErr != nil {
			assert.FailNow(ginkgo.GinkgoT(), "failed to create user: %s", createErr.Error())
		}
		return created
	}

	ginkgo.Describe("UpdatePassword", func() {
		ginkgo.It("updates the password hash of an existing user", func() {
			created := createUser()

			actual, actualErr := userWriteDatastoreAdapter.UpdatePassword(ctx, identity.UpdateUserPasswordRequest{
				ID:           created.ID().String(),
				PasswordHash: "rotated-hash",
			})
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), created.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), "rotated-hash", actual.PasswordHash())
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			_, actualErr := userWriteDatastoreAdapter.UpdatePassword(ctx, identity.UpdateUserPasswordRequest{
				ID:           entity.NewID().String(),
				PasswordHash: "rotated-hash",
			})
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})

	ginkgo.Describe("Update status", func() {
		ginkgo.It("persists a status transition made by the aggregate", func() {
			created := createUser()
			assert.NoError(ginkgo.GinkgoT(), created.MarkDeleted())

			_, updateErr := userWriteDatastoreAdapter.Update(ctx, created)
			assert.NoError(ginkgo.GinkgoT(), updateErr)

			readDatastore := NewUserReadDatastoreAdapter(testPostgresDatabaseClient, database.Config{
				OperationTimeout: 5 * time.Minute,
				RetryConfig:      recoveryutils.DefaultRetryConfig,
			})
			actual, actualErr := readDatastore.FetchById(ctx, created.ID().String())
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), identity.UserStatusDeleted, actual.Status())
		})
	})

	ginkgo.Describe("SoftDelete", func() {
		ginkgo.It("hides a soft-deleted user from reads", func() {
			created := createUser()

			assert.NoError(ginkgo.GinkgoT(), userWriteDatastoreAdapter.SoftDelete(ctx, created.ID().String()))

			readDatastore := NewUserReadDatastoreAdapter(testPostgresDatabaseClient, database.Config{
				OperationTimeout: 5 * time.Minute,
				RetryConfig:      recoveryutils.DefaultRetryConfig,
			})
			_, actualErr := readDatastore.FetchById(ctx, created.ID().String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			actualErr := userWriteDatastoreAdapter.SoftDelete(ctx, entity.NewID().String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})

	ginkgo.Describe("Delete", func() {
		ginkgo.It("permanently removes an existing user", func() {
			created := createUser()

			assert.NoError(ginkgo.GinkgoT(), userWriteDatastoreAdapter.Delete(ctx, created.ID().String()))

			readDatastore := NewUserReadDatastoreAdapter(testPostgresDatabaseClient, database.Config{
				OperationTimeout: 5 * time.Minute,
				RetryConfig:      recoveryutils.DefaultRetryConfig,
			})
			_, actualErr := readDatastore.FetchById(ctx, created.ID().String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})

		ginkgo.It("returns not found when the user does not exist", func() {
			actualErr := userWriteDatastoreAdapter.Delete(ctx, entity.NewID().String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})
})
