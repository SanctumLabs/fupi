//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	"parksys/internal/pkg/sharedkernel"
	"parksys/pkg/infra/cache"
	redisClient "parksys/pkg/infra/redis"

	"github.com/testcontainers/testcontainers-go"
	redisContainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestRedisCache(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Redis Cache Client Suite")
}

var _ = ginkgo.Describe("Redis Cache Client", ginkgo.Ordered, func() {
	ctx := context.Background()

	var (
		container   *redisContainer.RedisContainer
		client      *redisClient.RedisClient
		cacheClient cache.CacheClient
	)

	ginkgo.BeforeAll(func() {
		var err error
		container, err = redisContainer.RunContainer(ctx,
			testcontainers.WithImage("docker.io/redis:7"),
			redisContainer.WithSnapshotting(10, 1),
			redisContainer.WithLogLevel(redisContainer.LogLevelVerbose),
		)

		if err != nil {
			panic(err)
		}

		redisHost, err := container.Host(ctx)
		if err != nil {
			panic(err)
		}

		port, err := container.MappedPort(ctx, nat.Port("6379"))
		if err != nil {
			panic(err)
		}

		client = redisClient.NewRedisClient(redisClient.RedisClientParams{
			Address:  []string{fmt.Sprintf("%s:%s", redisHost, port.Port())},
			Database: 0,
		})

		cacheClient = NewRedisCache(*client)
	})

	ginkgo.AfterAll(func() {
		if err := container.Terminate(ctx); err != nil {
			panic(fmt.Sprintf("failed to terminate redis container: %s", err))
		}
	})

	type object struct {
		sharedkernel.Entity
		name string
	}

	ginkgo.Describe("Adding items to cache", func() {

		// TODO: fix failing integration test for retrieval of cache item
		ginkgo.XIt("should return nil error on successful addition of item to cache", func() {
			objectEntity := sharedkernel.NewEntity()

			itemToCache := object{
				Entity: objectEntity,
				name:   "Item",
			}
			itemId := objectEntity.ID.String()

			err := cacheClient.Set(ctx, cache.CacheItem{Key: itemId, Value: itemToCache})
			assert.NoError(ginkgo.GinkgoT(), err)

			// check that item was added to cache
			var payload object
			cacheItem, err := cacheClient.Get(ctx, itemId, &payload)
			assert.NoError(ginkgo.GinkgoT(), err)
			assert.NotNil(ginkgo.GinkgoT(), cacheItem.Value)

			assert.Equal(ginkgo.GinkgoT(), &itemToCache, cacheItem.Value)
		})
	})

	ginkgo.Describe("Checking existence of items", func() {

		ginkgo.It("should return true on successful check of an existing item in cache", func() {
			objectEntity := sharedkernel.NewEntity()

			itemToCache := object{
				Entity: objectEntity,
				name:   "Item",
			}

			itemId := objectEntity.ID.String()

			err := cacheClient.Set(ctx, cache.CacheItem{Key: itemId, Value: itemToCache})
			assert.NoError(ginkgo.GinkgoT(), err)

			actual := cacheClient.Exists(ctx, itemId)
			assert.True(ginkgo.GinkgoT(), actual)
		})

		ginkgo.It("should return false on successful check of an existing item in cache", func() {
			objectEntity := sharedkernel.NewEntity()

			itemId := objectEntity.ID.String()

			actual := cacheClient.Exists(ctx, itemId)
			assert.False(ginkgo.GinkgoT(), actual)
		})
	})

	ginkgo.Describe("Deleting cache items", func() {

		ginkgo.It("should return nil error on successful deletion of an existing item in cache", func() {
			objectEntity := sharedkernel.NewEntity()

			itemToCache := object{
				Entity: objectEntity,
				name:   "Item",
			}

			itemId := objectEntity.ID.String()

			err := cacheClient.Set(ctx, cache.CacheItem{Key: itemId, Value: itemToCache})
			assert.NoError(ginkgo.GinkgoT(), err)

			actual := cacheClient.Delete(ctx, itemId)
			assert.NoError(ginkgo.GinkgoT(), actual)
		})

		ginkgo.It("should return nil even if key does not exist in cache", func() {
			objectEntity := sharedkernel.NewEntity()

			itemId := objectEntity.ID.String()

			actual := cacheClient.Delete(ctx, itemId)
			assert.NoError(ginkgo.GinkgoT(), actual)
		})
	})
})
