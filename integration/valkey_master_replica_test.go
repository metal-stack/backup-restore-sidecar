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

func Test_Valkey_MasterReplica_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     "valkey-master-replica",
		sts:              examples.ValkeyMasterReplicaSts,
		backingResources: examples.ValkeyMasterReplicaBackingResources,
		addTestData:      addValkeyMasterReplicaTestData,
		verifyTestData:   verifyValkeyMasterReplicaTestData,
	})
}

func Test_Valkey_MasterReplica_RestoreLatestFromMultipleBackups(t *testing.T) {
	restoreLatestFromMultipleBackupsFlow(t, &flowSpec{
		databaseType:            "valkey-master-replica",
		sts:                     examples.ValkeyMasterReplicaSts,
		backingResources:        examples.ValkeyMasterReplicaBackingResources,
		addTestDataWithIndex:    addValkeyMasterReplicaTestDataWithIndex,
		verifyTestDataWithIndex: verifyValkeyMasterReplicaTestDataWithIndex,
	})
}

func newValkeyMasterReplicaClient(t *testing.T, ctx context.Context) *redis.Client {
	var cli *redis.Client

	err := retry.Do(func() error {
		cli = redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})
		_, err := cli.Ping(ctx).Result()
		return err
	}, retry.Context(ctx))
	require.NoError(t, err)

	return cli
}

func addValkeyMasterReplicaTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyMasterReplicaClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(ctx, "valkey-master-replica-test", "I am precious master-replica data", 1*time.Hour).Result()
	require.NoError(t, err)
}

func verifyValkeyMasterReplicaTestData(t *testing.T, ctx context.Context) {
	cli := newValkeyMasterReplicaClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	resp, err := cli.Get(ctx, "valkey-master-replica-test").Result()
	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, "I am precious master-replica data", resp)
}

func addValkeyMasterReplicaTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyMasterReplicaClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	_, err := cli.Set(
		ctx,
		fmt.Sprintf("valkey-master-replica-%d", index),
		fmt.Sprintf("valkey-master-replica-idx-%d", index),
		1*time.Hour).
		Result()
	require.NoError(t, err)
}

func verifyValkeyMasterReplicaTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	cli := newValkeyMasterReplicaClient(t, ctx)
	defer func() {
		_ = cli.Close()
	}()

	var resp string
	err := retry.Do(func() error {
		var err error
		resp, err = cli.Get(ctx, fmt.Sprintf("valkey-master-replica-%d", index)).Result()
		return err
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))

	require.NoError(t, err)
	require.NotEmpty(t, resp)
	assert.Equal(t, fmt.Sprintf("valkey-master-replica-idx-%d", index), resp)
}
