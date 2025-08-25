//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
)

func Test_Valkey_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Valkey,
		sts:              examples.ValkeySts,
		backingResources: examples.ValkeyBackingResources,
		addTestData:      addValkeyTestData,
		verifyTestData:   verifyValkeyTestData,
	})
}

func Test_Valkey_RestoreLatestFromMultipleBackups(t *testing.T) {
	restoreLatestFromMultipleBackupsFlow(t, &flowSpec{
		databaseType:            examples.Valkey,
		sts:                     examples.ValkeySts,
		backingResources:        examples.ValkeyBackingResources,
		addTestDataWithIndex:    addValkeyTestDataWithIndex,
		verifyTestDataWithIndex: verifyValkeyTestDataWithIndex,
	})
}

func newValkeyClient(t *testing.T, ctx context.Context) *redis.Client {
	var cli *redis.Client

	err := retry.Do(func() error {
		cli = redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})
		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return cli
}

func addValkeyTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(ctx, "valkey-test", "I am precious data", 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyValkeyTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, "valkey-test").Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, "I am precious data", resp)
}

func addValkeyTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(
		ctx,
		fmt.Sprintf("valkey-%d", index),
		fmt.Sprintf("valkey-idx-%d", index),
		1*time.Hour).
		Result()
	require.NoError(t, err)
}

func verifyValkeyTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, fmt.Sprintf("valkey-%d", index)).Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, fmt.Sprintf("valkey-idx-%d", index), resp)
}
