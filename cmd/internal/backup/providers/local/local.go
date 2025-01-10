package local

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
)

const (
	defaultLocalBackupPath = constants.SidecarBaseDir + "/local-provider"
)

// BackupProviderLocal implements the backup provider interface for no backup provider (useful to disable sidecar functionality in development environments)
type BackupProviderLocal struct {
	fs              afero.Fs
	log             *slog.Logger
	config          *BackupProviderConfigLocal
	nextBackupCount int64
}

// BackupProviderConfigLocal provides configuration for the BackupProviderLocal
type BackupProviderConfigLocal struct {
	LocalBackupPath string
	ObjectsToKeep   int64
	FS              afero.Fs
}

func (c *BackupProviderConfigLocal) validate() error {
	return nil
}

// New returns a Local backup provider
func New(log *slog.Logger, config *BackupProviderConfigLocal) (*BackupProviderLocal, error) {
	if config == nil {
		return nil, errors.New("local backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.LocalBackupPath == "" {
		config.LocalBackupPath = defaultLocalBackupPath
	}
	if config.FS == nil {
		config.FS = afero.NewOsFs()
	}

	err := config.validate()
	if err != nil {
		return nil, err
	}

	return &BackupProviderLocal{
		config: config,
		log:    log,
		fs:     config.FS,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderLocal) EnsureBackupBucket(_ context.Context) error {
	b.log.Info("ensuring backup bucket called for provider local")

	if err := b.fs.MkdirAll(b.config.LocalBackupPath, 0777); err != nil {
		return fmt.Errorf("could not create local backup directory: %w", err)
	}

	return nil
}

// CleanupBackups cleans up backups according to the given backup cleanup policy at the backup provider
func (b *BackupProviderLocal) CleanupBackups(_ context.Context) error {
	b.log.Info("cleanup backups called for provider local")

	return nil
}

// DownloadBackup downloads the given backup version to the specified folder
func (b *BackupProviderLocal) DownloadBackup(_ context.Context, version *providers.BackupVersion, outDir string) (string, error) {
	b.log.Info("download backup called for provider local")

	source := filepath.Join(b.config.LocalBackupPath, version.Name)

	backupFilePath := filepath.Join(outDir, version.Name)

	err := utils.Copy(b.fs, source, backupFilePath)
	if err != nil {
		return "", err
	}

	return backupFilePath, err
}

// UploadBackup uploads a backup to the backup provider
func (b *BackupProviderLocal) UploadBackup(_ context.Context, sourcePath string) error {
	b.log.Info("upload backups called for provider local")

	destination := filepath.Join(b.config.LocalBackupPath, filepath.Base(sourcePath))

	err := utils.Copy(b.fs, sourcePath, destination)
	if err != nil {
		return err
	}

	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderLocal) GetNextBackupName(_ context.Context) string {
	name := strconv.FormatInt(b.nextBackupCount, 10)
	b.nextBackupCount++
	b.nextBackupCount = b.nextBackupCount % b.config.ObjectsToKeep
	return name
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderLocal) ListBackups(_ context.Context) (providers.BackupVersions, error) {
	b.log.Info("listing backups called for provider local")

	d, err := b.fs.Open(b.config.LocalBackupPath)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	var files []os.FileInfo
	for _, name := range names {
		info, err := b.fs.Stat(filepath.Join(b.config.LocalBackupPath, name))
		if err != nil {
			return nil, err
		}
		files = append(files, info)
	}

	return backupVersionsLocal{
		files: files,
	}, nil
}
