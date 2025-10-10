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

func Test_Valkey_Cluster_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Valkey,
		sts:              examples.ValkeySts,
		backingResources: examples.ValkeyBackingResources,
		addTestData:      addValkeyClusterTestData,
		verifyTestData:   verifyValkeyClusterTestData,
	})
}

func Test_Valkey_Cluster_RestoreLatestFromMultipleBackups(t *testing.T) {
	restoreLatestFromMultipleBackupsFlow(t, &flowSpec{
		databaseType:            examples.Valkey,
		sts:                     examples.ValkeySts,
		backingResources:        examples.ValkeyBackingResources,
		addTestDataWithIndex:    addValkeyClusterTestDataWithIndex,
		verifyTestDataWithIndex: verifyValkeyClusterTestDataWithIndex,
	})
}

func newValkeyClusterClient(t *testing.T, ctx context.Context) *redis.Client {
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

func addValkeyClusterTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyClusterClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(ctx, "valkey-cluster-test", "I am precious cluster data", 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyValkeyClusterTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyClusterClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, "valkey-cluster-test").Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, "I am precious cluster data", resp)
}

func addValkeyClusterTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyClusterClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(
		ctx,
		fmt.Sprintf("valkey-cluster-%d", index),
		fmt.Sprintf("valkey-cluster-idx-%d", index),
		1*time.Hour).
		Result()
	require.NoError(t, err)
}

func verifyValkeyClusterTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyClusterClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, fmt.Sprintf("valkey-cluster-%d", index)).Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, fmt.Sprintf("valkey-cluster-idx-%d", index), resp)
}
