//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"

	_ "github.com/lib/pq"
)

func Test_ETCD_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.EtcdDatabaseName,
		sts:              examples.EtcdSts,
		backingResources: examples.EtcdBackingResources,
		addTestData:      addEtcdTestData,
		verifyTestData:   verifyEtcdTestData,
	})
}

func newEtcdClient(t *testing.T, ctx context.Context) *clientv3.Client {
	var cli *clientv3.Client

	err := retry.Do(func() error {
		var err error
		cli, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{"localhost:32379"},
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			return err
		}

		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return cli
}

func addEtcdTestData(t *testing.T, ctx context.Context) {
	cli := newEtcdClient(t, ctx)
	defer cli.Close()

	_, err := cli.Put(ctx, "1", "I am precious")
	require.NoError(t, err)
}

func verifyEtcdTestData(t *testing.T, ctx context.Context) {
	cli := newEtcdClient(t, ctx)
	defer cli.Close()

	resp, err := cli.Get(ctx, "1")
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1)

	ev := resp.Kvs[0]
	assert.Equal(t, "1", string(ev.Key))
	assert.Equal(t, "I am precious", string(ev.Value))
}
