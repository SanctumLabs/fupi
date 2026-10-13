package redis

import (
	"context"
	"testing"
	"time"

	redisGo "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRedisClient_RejectsAnEmptyAddressList(t *testing.T) {
	_, err := NewRedisClient(RedisClientConfig{})

	require.Error(t, err)
}

// The client dials lazily and go-redis reconnects by itself, so building it must succeed while Redis is down.
func TestNewRedisClient_BuildsWithoutIOAndPingReportsAnUnreachableRedis(t *testing.T) {
	client, err := NewRedisClient(RedisClientConfig{Address: []string{"127.0.0.1:1"}})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	assert.Error(t, client.Ping(ctx))
	assert.NoError(t, client.Close())
}

func TestNewRedisClient_PicksTheClientTypeFromTheAddressCount(t *testing.T) {
	single, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001"}})
	require.NoError(t, err)
	assert.IsType(t, &redisGo.Client{}, single.(*redisClient).client)

	cluster, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001", "localhost:7002"}})
	require.NoError(t, err)
	assert.IsType(t, &redisGo.ClusterClient{}, cluster.(*redisClient).client)
}

func TestUniversalClient_ExposesTheClientOfThisPackage(t *testing.T) {
	client, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001"}})
	require.NoError(t, err)

	universal, ok := UniversalClient(client)

	require.True(t, ok)
	assert.Same(t, client.(*redisClient).client, universal)
}

func TestUniversalClient_ReportsAForeignCacheClient(t *testing.T) {
	universal, ok := UniversalClient(nil)

	assert.False(t, ok)
	assert.Nil(t, universal)
}

// Without ContextTimeoutEnabled go-redis ignores the deadline of a command's context, so a caller's timeout on a hung
// Redis would last as long as the client's read timeout.
func TestNewRedisClient_HonoursContextDeadlines(t *testing.T) {
	client, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001"}})
	require.NoError(t, err)

	assert.True(t, client.(*redisClient).client.(*redisGo.Client).Options().ContextTimeoutEnabled)
}
