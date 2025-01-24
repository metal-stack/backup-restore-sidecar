//go:build integration

package gcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/api/option"
)

func Test_BackupProviderGCP(t *testing.T) {
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		log         = slog.Default()
	)

	defer cancel()

	c, conn := startFakeGcsContainer(t, ctx)
	defer func() {
		if t.Failed() {
			r, err := c.Logs(ctx)
			require.NoError(t, err)

			if err == nil {
				logs, err := io.ReadAll(r)
				require.NoError(t, err)

				fmt.Println(string(logs))
			}
		}
		err := c.Terminate(ctx)
		require.NoError(t, err)
	}()

	var (
		endpoint           = conn.Endpoint + "/storage/v1/"
		backupAmount       = 5
		expectedBackupName = "db.tar.gz"
		prefix             = fmt.Sprintf("test-with-%d", backupAmount)

		fs = afero.NewMemMapFs()

		transCfg = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
		httpClient = &http.Client{Transport: transCfg}
	)

	compressor, err := compress.New("targz")
	require.NoError(t, err)

	p, err := New(ctx, log, &BackupProviderConfigGCP{
		BucketName:     "test",
		BucketLocation: "europe-west3",
		ObjectPrefix:   prefix,
		ProjectID:      "test-project-id",
		FS:             fs,
		ClientOpts:     []option.ClientOption{option.WithEndpoint(endpoint), option.WithHTTPClient(httpClient)},
		Suffix:         compressor.Extension(),
	})

	require.NoError(t, err)
	require.NotNil(t, p)

	t.Run("ensure backup bucket", func(t *testing.T) {
		err := p.EnsureBackupBucket(ctx)
		require.NoError(t, err)
	})

	if t.Failed() {
		return
	}

	t.Run("verify upload", func(t *testing.T) {
		for i := range backupAmount {
			backupName := p.GetNextBackupName(ctx) + ".tar.gz"
			assert.Equal(t, expectedBackupName, backupName)

			backupPath := path.Join(constants.UploadDir, backupName)
			backupContent := fmt.Sprintf("precious data %d", i)

			err = afero.WriteFile(fs, backupPath, []byte(backupContent), 0600)
			require.NoError(t, err)

			backupFile, err := fs.Open(backupPath)
			require.NoError(t, err)
			err = p.UploadBackup(ctx, backupFile)
			require.NoError(t, err)

			// cleaning up after test
			err = fs.Remove(backupPath)
			require.NoError(t, err)
		}
	})

	if t.Failed() {
		return
	}

	t.Run("list backups", func(t *testing.T) {
		versions, err := p.ListBackups(ctx)
		require.NoError(t, err)

		_, err = versions.Get("foo")
		require.Error(t, err)

		allVersions := versions.List()
		// even if the amount is larger than max backups to keep the fake server
		// does not clean it up with lifecycle management
		require.Len(t, allVersions, backupAmount)

		for i, v := range allVersions {
			v := v

			fmt.Println(v)

			assert.True(t, strings.HasSuffix(v.Name, ".tar.gz"))
			assert.NotZero(t, v.Date)

			getVersion, err := versions.Get(v.Version)
			require.NoError(t, err)
			assert.Equal(t, v, getVersion)

			if i == 0 {
				continue
			}
			assert.True(t, v.Date.Before(allVersions[i-1].Date))
		}

		latestVersion := versions.Latest()
		assert.Equal(t, allVersions[0], latestVersion)
	})

	if t.Failed() {
		return
	}

	t.Run("verify download", func(t *testing.T) {
		versions, err := p.ListBackups(ctx)
		require.NoError(t, err)

		latestVersion := versions.Latest()
		require.NotNil(t, latestVersion)

		outputFile, err := fs.Create("outputfile")
		require.NoError(t, err)

		err = p.DownloadBackup(ctx, latestVersion, outputFile)
		require.NoError(t, err)

		gotContent, err := afero.ReadFile(fs, outputFile.Name())
		require.NoError(t, err)

		backupContent := fmt.Sprintf("precious data %d", backupAmount-1)
		require.Equal(t, backupContent, string(gotContent))

		// cleaning up after test
		err = fs.Remove(outputFile.Name())
		require.NoError(t, err)
	})

	if t.Failed() {
		return
	}

	t.Run("verify cleanup", func(t *testing.T) {
		err := p.CleanupBackups(ctx)
		require.NoError(t, err)
	})

	if t.Failed() {
		return
	}

	err = afero.Walk(fs, "/", func(path string, info iofs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		return fmt.Errorf("provider messed around in the file system at: %s", path)
	})
	require.NoError(t, err)
}

type connectionDetails struct {
	Endpoint string
}

func startFakeGcsContainer(t testing.TB, ctx context.Context) (testcontainers.Container, *connectionDetails) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "fsouza/fake-gcs-server", // tested with fsouza/fake-gcs-server:1.47.4
			// ExposedPorts: []string{"4443"},
			HostConfigModifier: func(hc *container.HostConfig) {
				// Unfortunately we must use host network as the public host must exactly match the client endpoint
				// see for example: https://github.com/fsouza/fake-gcs-server/issues/196
				//
				// without it the download does not work because the server directs to the wrong (public?) endpoint
				hc.NetworkMode = "host"
			},
			Cmd: []string{"-backend", "memory", "-log-level", "debug", "-public-host", "localhost:4443"},
			WaitingFor: wait.ForAll(
				// wait.ForListeningPort("4443/tcp"),
				wait.ForLog("server started"),
			),
		},
		Started: true,
		Logger:  testcontainers.TestLogger(t),
	})
	require.NoError(t, err)

	conn := &connectionDetails{
		Endpoint: "https://localhost:4443",
	}

	return c, conn
}
