//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"
	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	meilisearchIndex = "backup-restore-sidecar"
)

func Test_Meilisearch_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     examples.Meilisearch,
		sts:              examples.MeilisearchSts,
		backingResources: examples.MeilisearchBackingResources,
		addTestData:      addMeilisearchTestData,
		verifyTestData:   verifyMeilisearchTestData,
	})
}

func Test_Meilisearch_Upgrade(t *testing.T) {
	upgradeFlow(t, &upgradeFlowSpec{
		flowSpec: flowSpec{
			databaseType:     examples.Meilisearch,
			sts:              examples.MeilisearchSts,
			backingResources: examples.MeilisearchBackingResources,
			addTestData:      addMeilisearchTestData,
			verifyTestData:   verifyMeilisearchTestData,
		},
		databaseImages: []string{
			"getmeili/meilisearch:v1.2.0",
			"getmeili/meilisearch:v1.3.0",
			"getmeili/meilisearch:v1.3.2",
		},
	})
}

func newMeilisearchSession(t *testing.T, ctx context.Context) *meilisearch.Client {
	var client *meilisearch.Client
	err := retry.Do(func() error {

		client = meilisearch.NewClient(meilisearch.ClientConfig{
			Host:   "http://localhost:7700",
			APIKey: examples.MeilisearchPassword,
		})

		ok := client.IsHealthy()
		if !ok {
			return fmt.Errorf("meilisearch is not yet healthy")
		}
		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return client
}

func addMeilisearchTestData(t *testing.T, ctx context.Context) {
	client := newMeilisearchSession(t, ctx)
	creationTask, err := client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        meilisearchIndex,
		PrimaryKey: "id",
	})
	require.NoError(t, err)
	_, err = client.WaitForTask(creationTask.TaskUID)
	require.NoError(t, err)

	index := client.Index(meilisearchIndex)
	testdata := map[string]any{
		"id":  "1",
		"key": "I am precious",
	}
	indexTask, err := index.AddDocuments(testdata, "id")
	require.NoError(t, err)
	_, err = client.WaitForTask(indexTask.TaskUID)
	require.NoError(t, err)
}

func verifyMeilisearchTestData(t *testing.T, ctx context.Context) {
	client := newMeilisearchSession(t, ctx)
	index, err := client.GetIndex(meilisearchIndex)
	require.NoError(t, err)
	testdata := make(map[string]any)
	err = index.GetDocument("1", &meilisearch.DocumentQuery{}, &testdata)
	require.NoError(t, err)
	assert.Equal(t, "I am precious", testdata["key"])
}
