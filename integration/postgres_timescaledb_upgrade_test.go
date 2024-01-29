//go:build integration

package integration_test

import (
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	_ "github.com/lib/pq"
)

func Test_Postgres_TimescaleDB_Upgrade(t *testing.T) {
	backingResources := examples.PostgresBackingResources(namespaceName(t))

	modified := false

	for _, r := range backingResources {
		cm, ok := r.(*corev1.ConfigMap)
		if !ok {
			continue
		}

		if cm.Name != "backup-restore-sidecar-config-postgres" {
			continue
		}

		cm.Data = map[string]string{
			"config.yaml": `---
bind-addr: 0.0.0.0
db: postgres
db-data-directory: /data/postgres/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: postgres-test
compression-method: tar
post-exec-cmds:
- docker-entrypoint.sh postgres  -c shared_preload_libraries=timescaledb
`}

		modified = true
		break
	}

	require.True(t, modified)

	upgradeFlow(t, &upgradeFlowSpec{
		flowSpec: flowSpec{
			databaseType: examples.Postgres,
			sts:          examples.PostgresSts,
			backingResources: func(namespace string) []client.Object {
				return backingResources
			},
			addTestData:    addPostgresTestData,
			verifyTestData: verifyPostgresTestData,
		},
		databaseImages: []string{
			"timescale/timescaledb:2.11.2-pg12",
			"timescale/timescaledb:2.11.2-pg15",
		},
	})
}
