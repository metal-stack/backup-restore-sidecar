//go:build integration

package integration_test

import (
	"context"
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
- docker-entrypoint.sh postgres -c shared_preload_libraries=timescaledb
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
			addTestData:    addTimescaleDbTestData,
			verifyTestData: verifyPostgresTestData,
		},
		databaseImages: []string{
			"timescale/timescaledb:2.11.2-pg12",
			"timescale/timescaledb:2.11.2-pg15",
			// it is allowed to skip a minor version
			// "timescale/timescaledb:2.12.2-pg15",
			"timescale/timescaledb:2.13.1-pg15",
			"timescale/timescaledb:2.13.1-pg16",
		},
	})
}

func addTimescaleDbTestData(t *testing.T, ctx context.Context) {
	db := newPostgresSession(t, ctx)
	defer db.Close()

	var (
		createStmt = `

        CREATE EXTENSION IF NOT EXISTS timescaledb;

        CREATE TABLE IF NOT EXISTS backuprestore (
            timestamp timestamp NOT NULL,
            data text NOT NULL,
            PRIMARY KEY(timestamp, data)
        );
        SELECT create_hypertable('backuprestore', 'timestamp', chunk_time_interval => INTERVAL '1 days', if_not_exists => TRUE);

        ALTER TABLE backuprestore SET (
            timescaledb.compress,
            timescaledb.compress_segmentby = 'data',
            timescaledb.compress_orderby = 'timestamp'
        );
        SELECT add_compression_policy('backuprestore', INTERVAL '1 days');

        `
		insertStmt = `INSERT INTO backuprestore("timestamp", "data") VALUES ('2024-01-01 12:00:00.000', 'I am precious');`
	)

	_, err := db.Exec(createStmt)
	require.NoError(t, err)

	_, err = db.Exec(insertStmt)
	require.NoError(t, err)
}
