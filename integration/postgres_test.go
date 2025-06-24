//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

func Test_Postgres_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Postgres,
		sts:              examples.PostgresSts,
		backingResources: examples.PostgresBackingResources,
		addTestData:      addPostgresTestData,
		verifyTestData:   verifyPostgresTestData,
	})
}

func Test_Postgres_RestoreLatestFromMultipleBackups(t *testing.T) {
	restoreLatestFromMultipleBackupsFlow(t, &flowSpec{
		databaseType:            examples.Postgres,
		sts:                     examples.PostgresSts,
		backingResources:        examples.PostgresBackingResources,
		addTestDataWithIndex:    addPostgresTestDataWithIndex,
		verifyTestDataWithIndex: verifyPostgresTestDataWithIndex,
	})
}

func Test_Postgres_Upgrade(t *testing.T) {
	upgradeFlow(t, &upgradeFlowSpec{
		flowSpec: flowSpec{
			databaseType:     examples.Postgres,
			sts:              examples.PostgresSts,
			backingResources: examples.PostgresBackingResources,
			addTestData:      addPostgresTestData,
			verifyTestData:   verifyPostgresTestData,
		},
		databaseImages: []string{
			"postgres:12-alpine",
			// Upgrade from 12-alpine to 13-alpine is not possible because of library differences in icu-lib.
			// The solution is to upgrade to a older 14.10-alpine which has the same icu-lib version as 12-alpine
			// and then update to 14.18-alpine or newer which does not require to run pg_upgrade.
			// "postgres:13-alpine",
			"postgres:14.10-alpine",
			"postgres:14.18-alpine",
			"postgres:15-alpine",
			"postgres:17-alpine",
		},
	})
}

func newPostgresSession(t *testing.T, ctx context.Context) *sql.DB {
	var db *sql.DB
	err := retry.Do(func() error {
		connString := fmt.Sprintf("host=127.0.0.1 port=5432 user=%s password=%s dbname=%s sslmode=disable", examples.PostgresUser, examples.PostgresPassword, examples.PostgresDB)

		var err error
		db, err = sql.Open("postgres", connString)
		if err != nil {
			return err
		}

		err = db.PingContext(ctx)
		if err != nil {
			return err
		}

		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return db
}

func addPostgresTestData(t *testing.T, ctx context.Context) {
	db := newPostgresSession(t, ctx)
	defer func() {
		_ = db.Close()
	}()

	var (
		createStmt = `CREATE TABLE backuprestore (
			data text NOT NULL
		 );`
		insertStmt = `INSERT INTO backuprestore("data") VALUES ('I am precious');`
	)

	_, err := db.Exec(createStmt)
	require.NoError(t, err)

	_, err = db.Exec(insertStmt)
	require.NoError(t, err)
}

func addPostgresTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	db := newPostgresSession(t, ctx)
	defer func() {
		_ = db.Close()
	}()

	var (
		createStmt = `CREATE TABLE IF NOT EXISTS backuprestore (
			data text NOT NULL
		 );`
		insertStmt = fmt.Sprintf("INSERT INTO backuprestore (data) VALUES ('idx-%d');", index)
	)

	_, err := db.Exec(createStmt)
	require.NoError(t, err)

	_, err = db.Exec(insertStmt)
	require.NoError(t, err)
}

func verifyPostgresTestDataWithIndex(t *testing.T, ctx context.Context, index int) {
	db := newPostgresSession(t, ctx)
	defer func() {
		_ = db.Close()
	}()

	rows, err := db.Query(fmt.Sprintf("SELECT \"data\" FROM backuprestore WHERE data='idx-%d';", index))
	require.NoError(t, err)
	require.NoError(t, rows.Err())
	defer func() {
		_ = rows.Close()
	}()

	require.True(t, rows.Next())
	var data string

	err = rows.Scan(&data)
	require.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("idx-%d", index), data)
	assert.False(t, rows.Next())
}
func verifyPostgresTestData(t *testing.T, ctx context.Context) {
	db := newPostgresSession(t, ctx)
	defer func() {
		_ = db.Close()
	}()

	rows, err := db.Query(`SELECT "data" FROM backuprestore;`)
	require.NoError(t, err)
	require.NoError(t, rows.Err())
	defer func() {
		_ = rows.Close()
	}()

	require.True(t, rows.Next())
	var data string

	err = rows.Scan(&data)
	require.NoError(t, err)

	assert.Equal(t, "I am precious", data)
	assert.False(t, rows.Next())
}
