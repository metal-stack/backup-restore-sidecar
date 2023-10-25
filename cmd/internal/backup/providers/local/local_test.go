package local

import (
	"context"
	"fmt"
	iofs "io/fs"
	"path"
	"strings"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func Test_BackupProviderLocal(t *testing.T) {
	var (
		ctx                     = context.Background()
		localProviderBackupPath = defaultLocalBackupPath
		log                     = zaptest.NewLogger(t).Sugar()
	)

	for _, backupAmount := range []int{0, 1, 5, constants.DefaultObjectsToKeep + 5} {
		t.Run(fmt.Sprintf("testing with %d backups", backupAmount), func(t *testing.T) {
			fs := afero.NewMemMapFs()

			p, err := New(log, &BackupProviderConfigLocal{
				FS: fs,
			})
			require.NoError(t, err)
			require.NotNil(t, p)

			t.Run("ensure backup bucket", func(t *testing.T) {
				err := p.EnsureBackupBucket(ctx)
				require.NoError(t, err)

				info, err := fs.Stat(defaultLocalBackupPath)
				require.NoError(t, err)
				assert.True(t, info.IsDir())
			})

			if t.Failed() {
				return
			}

			t.Run("verify upload", func(t *testing.T) {
				for i := 0; i < backupAmount; i++ {
					backupName := p.GetNextBackupName(ctx) + ".tar.gz"
					backupPath := path.Join(constants.UploadDir, backupName)
					backupContent := fmt.Sprintf("precious data %d", i)

					err = afero.WriteFile(fs, backupPath, []byte(backupContent), 0600)
					require.NoError(t, err)

					err = p.UploadBackup(ctx, backupPath)
					require.NoError(t, err)

					localPath := path.Join(localProviderBackupPath, backupName)
					_, err := fs.Stat(localPath)
					require.NoError(t, err)

					backupFiles, err := afero.ReadDir(fs, localProviderBackupPath)
					require.NoError(t, err)
					if i+1 > constants.DefaultObjectsToKeep {
						require.Len(t, backupFiles, constants.DefaultObjectsToKeep)
					} else {
						require.Len(t, backupFiles, i+1)
					}

					backedupContent, err := afero.ReadFile(fs, localPath)
					require.NoError(t, err)
					require.Equal(t, backupContent, string(backedupContent))

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
				amount := backupAmount
				if backupAmount > constants.DefaultObjectsToKeep {
					amount = constants.DefaultObjectsToKeep
				}
				require.Len(t, allVersions, amount)

				for i, v := range allVersions {
					v := v

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

				err = p.DownloadBackup(ctx, latestVersion)
				require.NoError(t, err)

				downloadPath := path.Join(constants.DownloadDir, latestVersion.Name)
				gotContent, err := afero.ReadFile(fs, downloadPath)
				require.NoError(t, err)

				require.Equal(t, fmt.Sprintf("precious data %d", backupAmount-1), string(gotContent))

				// cleaning up after test
				err = fs.Remove(downloadPath)
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
				if strings.HasPrefix(path, localProviderBackupPath) {
					return nil
				}

				return fmt.Errorf("provider messed around in the file system at: %s", path)
			})
			require.NoError(t, err)
		})
	}
}
