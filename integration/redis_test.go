//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
)

func Test_Redis_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Redis,
		sts:              examples.RedisSts,
		backingResources: examples.RedisBackingResources,
		addTestData:      addRedisTestData,
		verifyTestData:   verifyRedisTestData,
	})
}

func newRedisClient(t *testing.T, ctx context.Context) *redis.Client {
	var cli *redis.Client

	err := retry.Do(func() error {
		var err error
		cli = redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})
		if err != nil {
			return err
		}

		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return cli
}

func addRedisTestData(t *testing.T, ctx context.Context) {
	cli := newRedisClient(t, ctx)
	defer cli.Close()

	_, err := cli.Set(ctx, "1", "I am precious", 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyRedisTestData(t *testing.T, ctx context.Context) {
	cli := newRedisClient(t, ctx)
	defer cli.Close()

	resp, err := cli.Get(ctx, "1").Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, "I am precious", resp)
}
