//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Localfs_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Localfs,
		sts:              examples.LocalfsSts,
		backingResources: examples.LocalfsBackingResources,
		addTestData:      addLocalfsTestData,
		verifyTestData:   verifyLocalfsTestData,
	})
}

func addLocalfsTestData(t *testing.T, ctx context.Context) {
	namespace := namespaceName(t)
	_, err := execCommand(ctx, "localfs-0", namespace, "backup-restore-sidecar", []string{"sh", "-c", "echo 'I am precious' > /data/test.txt"})
	require.NoError(t, err)
}

func verifyLocalfsTestData(t *testing.T, ctx context.Context) {
	namespace := namespaceName(t)
	resp, err := execCommand(ctx, "localfs-0", namespace, "backup-restore-sidecar", []string{"cat", "/data/test.txt"})
	require.NoError(t, err)

	assert.Equal(t, "I am precious", resp)
}
