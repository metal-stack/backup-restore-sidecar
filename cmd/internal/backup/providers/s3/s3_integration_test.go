// go:build integration

package s3

import (
	"context"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func Test_BackupProviderS3(t *testing.T) {
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		log         = slog.Default()
	)

	defer cancel()

	c, conn := startMinioContainer(t, ctx)
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
		endpoint           = conn.Endpoint
		backupAmount       = 5
		expectedBackupName = ""
		prefix             = fmt.Sprintf("test-with-%d", backupAmount)

		fs = afero.NewMemMapFs()
	)

	compressor, err := compress.New("targz")
	require.NoError(t, err)

	p, err := New(log, &BackupProviderConfigS3{
		BucketName:   "test",
		Endpoint:     endpoint,
		Region:       "dummy",
		AccessKey:    "ACCESSKEY",
		SecretKey:    "SECRETKEY",
		ObjectPrefix: prefix,
		FS:           fs,
		Compressor:   compressor,
	})
	expectedBackupName = p.GetNextBackupName(ctx) + ".tar.gz"
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

	if backupAmount <= 0 {
		return
	}

	t.Run("list backups", func(t *testing.T) {
		versions, err := p.ListBackups(ctx)
		require.NoError(t, err)

		_, err = versions.Get("foo")
		require.Error(t, err)

		allVersions := versions.List()
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

func startMinioContainer(t testing.TB, ctx context.Context) (testcontainers.Container, *connectionDetails) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "quay.io/minio/minio",
			ExposedPorts: []string{"9000"},
			Cmd:          []string{"server", "/data"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "ACCESSKEY",
				"MINIO_ROOT_PASSWORD": "SECRETKEY",
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("9000/tcp"),
			),
		},
		Started: true,
		Logger:  testcontainers.TestLogger(t),
	})
	require.NoError(t, err)

	host, err := c.Host(ctx)
	require.NoError(t, err)

	port, err := c.MappedPort(ctx, "9000")
	require.NoError(t, err)

	conn := &connectionDetails{
		Endpoint: "http://" + host + ":" + port.Port(),
	}

	return c, conn
}
