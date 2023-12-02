//go:build integration

package integration_test

import (
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
)

func Test_KeyDB_Cluster_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.KeyDB,
		sts:              examples.KeyDBClusterSts,
		backingResources: examples.KeyDBClusterBackingResources,
		addTestData:      addRedisTestData,
		verifyTestData:   verifyRedisTestData,
	})
}
