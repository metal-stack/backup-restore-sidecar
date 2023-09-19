//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avast/retry-go/v4"
	r "gopkg.in/rethinkdb/rethinkdb-go.v6"
)

const (
	rethinkDbDatabaseName = "backup-restore"
	rethinkDbTable        = "precioustestdata"
)

type rethinkDbTestData struct {
	ID   string `rethinkdb:"id"`
	Data string `rethinkdb:"data"`
}

func Test_RethinkDB_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.RethinkDB,
		sts:              examples.RethinkDbSts,
		backingResources: examples.RethinkDbBackingResources,
		addTestData:      addRethinkDbTestData,
		verifyTestData:   verifyRethinkDbTestData,
	})
}

func newRethinkdbSession(t *testing.T, ctx context.Context) *r.Session {
	var session *r.Session
	err := retry.Do(func() error {
		var err error
		session, err = r.Connect(r.ConnectOpts{
			Addresses: []string{"localhost:28015"},
			Database:  rethinkDbDatabaseName,
			Username:  "admin",
			Password:  examples.RethinkDbPassword,
			MaxIdle:   10,
			MaxOpen:   20,
		})
		if err != nil {
			return fmt.Errorf("cannot connect to DB: %w", err)
		}

		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return session
}

func addRethinkDbTestData(t *testing.T, ctx context.Context) {
	session := newRethinkdbSession(t, ctx)

	_, err := r.DBCreate(rethinkDbDatabaseName).RunWrite(session)
	require.NoError(t, err)

	_, err = r.DB(rethinkDbDatabaseName).TableCreate(rethinkDbTable).RunWrite(session)
	require.NoError(t, err)

	_, err = r.DB(rethinkDbDatabaseName).Table(rethinkDbTable).Insert(rethinkDbTestData{
		ID:   "1",
		Data: "i am precious",
	}).RunWrite(session)
	require.NoError(t, err)

	cursor, err := r.DB(rethinkDbDatabaseName).Table(rethinkDbTable).Get("1").Run(session)
	require.NoError(t, err)

	var d1 rethinkDbTestData
	err = cursor.One(&d1)
	require.NoError(t, err)
	require.Equal(t, "i am precious", d1.Data)
}

func verifyRethinkDbTestData(t *testing.T, ctx context.Context) {
	session := newRethinkdbSession(t, ctx)

	var d2 rethinkDbTestData
	err := retry.Do(func() error {
		cursor, err := r.DB(rethinkDbDatabaseName).Table(rethinkDbTable).Get("1").Run(session)
		if err != nil {
			return err
		}

		err = cursor.One(&d2)
		if err != nil {
			return err
		}

		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, "i am precious", d2.Data)
}
