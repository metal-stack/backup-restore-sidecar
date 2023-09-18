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
		databaseType:     "postgres",
		sts:              examples.PostgresSts,
		backingResources: examples.PostgresBackingResources,
		addTestData:      addPostgresTestData,
		verifyTestData:   verifyPostgresTestData,
	})
}

func Test_Postgres_Upgrade(t *testing.T) {
	upgradeFlow(t, &flowSpec{
		databaseType: "postgres",
		databaseImages: []string{
			"postgres:12-alpine",
			// "postgres:13-alpine", commented to test if two versions upgrade also work
			"postgres:14-alpine",
			"postgres:15-alpine",
		},
		sts:              examples.PostgresSts,
		backingResources: examples.PostgresBackingResources,
		addTestData:      addPostgresTestData,
		verifyTestData:   verifyPostgresTestData,
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
	defer db.Close()

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

func verifyPostgresTestData(t *testing.T, ctx context.Context) {
	db := newPostgresSession(t, ctx)
	defer db.Close()

	rows, err := db.Query(`SELECT "data" FROM backuprestore;`)
	require.NoError(t, err)
	require.NoError(t, rows.Err())
	defer rows.Close()

	require.True(t, rows.Next())
	var data string

	err = rows.Scan(&data)
	require.NoError(t, err)

	assert.Equal(t, "I am precious", data)
	assert.False(t, rows.Next())
}
