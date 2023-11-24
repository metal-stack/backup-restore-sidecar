//go:build integration

package integration_test

import (
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
)

func Test_KeyDB_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.KeyDB,
		sts:              examples.KeyDBSts,
		backingResources: examples.KeyDBBackingResources,
		addTestData:      addRedisTestData,
		verifyTestData:   verifyRedisTestData,
	})
}
