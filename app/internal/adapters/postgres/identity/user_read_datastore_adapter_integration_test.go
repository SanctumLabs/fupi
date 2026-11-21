//go:build integration

package identitydatastore

import (
	"context"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/onsi/ginkgo/v2"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/sanctumlabs/curtz/app/test"
	"github.com/stretchr/testify/assert"
)

var _ = ginkgo.Describe("User Read Datastore Adapter Integration Test Suite", ginkgo.Ordered, func() {
	ctx := context.Background()

	var (
		testPostgresDatabaseClient database.PostgresDatabaseClient
		userReadDatastoreAdapter   identity.UserReadDatastore
		userWriteDatastoreAdapter  identity.UserWriteDatastore
		seeded                     identity.User
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
		userReadDatastoreAdapter = NewUserReadDatastoreAdapter(testPostgresDatabaseClient, config)
		userWriteDatastoreAdapter = NewUserWriteDatastoreAdapter(testPostgresDatabaseClient, config)

		mockUser, mockUserErr := mockidentity.MockUser(
			mockidentity.WithUsername(faker.Username()),
			mockidentity.WithEmail(faker.Email()),
			mockidentity.WithStatus(identity.UserStatusInactive),
		)
		assert.NoError(ginkgo.GinkgoT(), mockUserErr)

		seeded, err = userWriteDatastoreAdapter.Save(ctx, *mockUser)
		if err != nil {
			assert.FailNow(ginkgo.GinkgoT(), "failed to seed user: %s", err.Error())
		}
	})

	ginkgo.AfterAll(func() {
		testPostgresDatabaseClient.Close()
	})

	ginkgo.Describe("FetchById", func() {
		ginkgo.It("returns the user for an existing id", func() {
			actual, actualErr := userReadDatastoreAdapter.FetchById(ctx, seeded.ID().String())
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), seeded.ID(), actual.ID())
			assert.Equal(ginkgo.GinkgoT(), seeded.Username(), actual.Username())
			assert.Equal(ginkgo.GinkgoT(), seeded.Email(), actual.Email())
			assert.Equal(ginkgo.GinkgoT(), seeded.PasswordHash(), actual.PasswordHash())
		})

		ginkgo.It("returns not found for an unknown id", func() {
			_, actualErr := userReadDatastoreAdapter.FetchById(ctx, entity.NewID().String())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})

	ginkgo.Describe("FetchByUsername", func() {
		ginkgo.It("returns the user for an existing username", func() {
			actual, actualErr := userReadDatastoreAdapter.FetchByUsername(ctx, seeded.Username())
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), seeded.ID(), actual.ID())
		})

		ginkgo.It("returns not found for an unknown username", func() {
			_, actualErr := userReadDatastoreAdapter.FetchByUsername(ctx, "no-such-user-"+faker.Username())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})

	ginkgo.Describe("FetchByEmail", func() {
		ginkgo.It("returns the user for an existing email", func() {
			email := seeded.Email()
			actual, actualErr := userReadDatastoreAdapter.FetchByEmail(ctx, email.Value())
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Equal(ginkgo.GinkgoT(), seeded.ID(), actual.ID())
		})

		ginkgo.It("returns not found for an unknown email", func() {
			_, actualErr := userReadDatastoreAdapter.FetchByEmail(ctx, faker.Email())
			assert.True(ginkgo.GinkgoT(), errdefs.IsNotFound(actualErr), "expected NotFound, got %v", actualErr)
		})
	})

	ginkgo.Describe("FetchAll", func() {
		ginkgo.It("returns a page of users including the seeded one", func() {
			actual, actualErr := userReadDatastoreAdapter.FetchAll(ctx, common.NewRequestParams())
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.GreaterOrEqual(ginkgo.GinkgoT(), actual.Total, 1)
			assert.Equal(ginkgo.GinkgoT(), 1, actual.Page)
			assert.Equal(ginkgo.GinkgoT(), len(actual.Records), actual.Size)

			found := false
			for _, user := range actual.Records {
				if user.ID() == seeded.ID() {
					found = true
				}
			}
			assert.True(ginkgo.GinkgoT(), found, "seeded user not returned by FetchAll")
		})

		ginkgo.It("respects the limit", func() {
			actual, actualErr := userReadDatastoreAdapter.FetchAll(ctx, common.NewRequestParams(common.WithRequestLimit(1)))
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Len(ginkgo.GinkgoT(), actual.Records, 1)
			assert.Equal(ginkgo.GinkgoT(), 1, actual.Size)
		})
	})

	ginkgo.Describe("FetchByStatus", func() {
		ginkgo.It("returns only users with the requested status", func() {
			// the seeded user is INACTIVE
			actual, actualErr := userReadDatastoreAdapter.FetchByStatus(ctx, identity.UserStatusInactive)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.GreaterOrEqual(ginkgo.GinkgoT(), actual.Total, 1)
			for _, user := range actual.Records {
				assert.Equal(ginkgo.GinkgoT(), identity.UserStatusInactive, user.Status())
			}
		})

		ginkgo.It("returns an empty page when no users have the status", func() {
			actual, actualErr := userReadDatastoreAdapter.FetchByStatus(ctx, identity.UserStatusDeleted)
			assert.NoError(ginkgo.GinkgoT(), actualErr)
			assert.Empty(ginkgo.GinkgoT(), actual.Records)
			assert.Equal(ginkgo.GinkgoT(), 0, actual.Total)
		})
	})
})
