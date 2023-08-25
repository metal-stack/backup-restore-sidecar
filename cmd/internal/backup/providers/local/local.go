package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"

	"go.uber.org/zap"
)

const (
	defaultLocalBackupPath = constants.SidecarBaseDir + "/local-provider"
)

// BackupProviderLocal implements the backup provider interface for no backup provider (useful to disable sidecar functionality in development environments)
type BackupProviderLocal struct {
	log             *zap.SugaredLogger
	config          *BackupProviderConfigLocal
	nextBackupCount int64
}

// BackupProviderConfigLocal provides configuration for the BackupProviderLocal
type BackupProviderConfigLocal struct {
	LocalBackupPath string
	ObjectsToKeep   int64
}

func (c *BackupProviderConfigLocal) validate() error {
	return nil
}

// New returns a Local backup provider
func New(log *zap.SugaredLogger, config *BackupProviderConfigLocal) (*BackupProviderLocal, error) {
	if config == nil {
		return nil, errors.New("local backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.LocalBackupPath == "" {
		config.LocalBackupPath = defaultLocalBackupPath
	}

	err := config.validate()
	if err != nil {
		return nil, err
	}

	return &BackupProviderLocal{
		config: config,
		log:    log,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderLocal) EnsureBackupBucket() error {
	b.log.Infow("ensuring backup bucket called for provider local")

	if err := os.MkdirAll(b.config.LocalBackupPath, 0777); err != nil {
		return fmt.Errorf("could not create local backup directory: %w", err)
	}

	return nil
}

// CleanupBackups cleans up backups according to the given backup cleanup policy at the backup provider
func (b *BackupProviderLocal) CleanupBackups() error {
	b.log.Infow("cleanup backups called for provider local")
	return nil
}

// DownloadBackup downloads the given backup version to the restoration folder
func (b *BackupProviderLocal) DownloadBackup(version *providers.BackupVersion) error {
	b.log.Infow("download backup called for provider local")
	source := filepath.Join(b.config.LocalBackupPath, version.Name)
	destination := filepath.Join(constants.DownloadDir, version.Name)
	err := utils.Copy(source, destination)
	if err != nil {
		return err
	}
	return nil
}

// UploadBackup uploads a backup to the backup provider
func (b *BackupProviderLocal) UploadBackup(sourcePath string) error {
	b.log.Infow("upload backups called for provider local")
	destination := filepath.Join(b.config.LocalBackupPath, filepath.Base(sourcePath))
	err := utils.Copy(sourcePath, destination)
	if err != nil {
		return err
	}
	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderLocal) GetNextBackupName() string {
	name := strconv.FormatInt(b.nextBackupCount, 10)
	b.nextBackupCount++
	b.nextBackupCount = b.nextBackupCount % b.config.ObjectsToKeep
	return name
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderLocal) ListBackups() (providers.BackupVersions, error) {
	b.log.Infow("listing backups called for provider local")
	d, err := os.Open(b.config.LocalBackupPath)
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
		info, err := os.Stat(filepath.Join(b.config.LocalBackupPath, name))
		if err != nil {
			return nil, err
		}
		files = append(files, info)
	}

	return BackupVersionsLocal{
		files: files,
	}, nil
}
