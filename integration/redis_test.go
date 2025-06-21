//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"strconv"
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

func Test_Redis_RestoreLatestFromMultipleBackups(t *testing.T) {
	restoreLatestFromMultipleBackupsFlow(t, &flowSpec{
		databaseType:            examples.Redis,
		sts:                     examples.RedisSts,
		backingResources:        examples.RedisBackingResources,
		addTestDataWithIndex:    addRedisTestDataWithIndex,
		verifyTestDataWithIndex: verifyRedisTestDataWithIndex,
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
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(ctx, "1", "I am precious", 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyRedisTestData(t *testing.T, ctx context.Context) {
	cli := newRedisClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, "1").Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, "I am precious", resp)
}

func addRedisTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newRedisClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(ctx, strconv.Itoa(index), fmt.Sprintf("idx-%d", index), 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyRedisTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newRedisClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, strconv.Itoa(index)).Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, fmt.Sprintf("idx-%d", index), resp)
}
