package local

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
)

const (
	defaultLocalBackupPath = constants.SidecarBaseDir + "/local-provider"
	defaultBackupName      = "db"
)

// BackupProviderLocal implements the backup provider interface for no backup provider (useful to disable sidecar functionality in development environments)
type BackupProviderLocal struct {
	fs                afero.Fs
	log               *slog.Logger
	config            *BackupProviderConfigLocal
	nextBackupCount   int64
	suffix            string
	currentBackupName string
}

// BackupProviderConfigLocal provides configuration for the BackupProviderLocal
type BackupProviderConfigLocal struct {
	LocalBackupPath string
	ObjectsToKeep   int64
	FS              afero.Fs
	Suffix          string
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
		suffix: config.Suffix,
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
func (b *BackupProviderLocal) DownloadBackup(_ context.Context, version *providers.BackupVersion, writer io.Writer) error {
	b.log.Info("download backup called for provider local")

	source := filepath.Join(b.config.LocalBackupPath, version.Name)

	infile, err := b.fs.Open(source)
	if err != nil {
		return fmt.Errorf("could not open file %s: %w", source, err)
	}
	defer infile.Close()

	_, err = io.Copy(writer, infile)
	if err != nil {
		return err
	}

	return err
}

// UploadBackup uploads a backup to the backup provider by providing a reader to the backup archive
func (b *BackupProviderLocal) UploadBackup(ctx context.Context, reader io.Reader) error {
	fmt.Println("starting upload")
	b.log.Info("upload backups called for provider local")

	destination := b.config.LocalBackupPath + "/" + b.currentBackupName + b.suffix
	fmt.Println("dest of provider file: ", "dest", destination)

	output, err := b.fs.Create(destination)
	if err != nil {
		return fmt.Errorf("could not create file %s: %w", destination, err)
	}

	_, err = io.Copy(output, reader)
	if err != nil {
		return err
	}

	fmt.Println("ending upload")

	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderLocal) GetNextBackupName(_ context.Context) string {
	name := strconv.FormatInt(b.nextBackupCount, 10)
	b.nextBackupCount++
	b.nextBackupCount = b.nextBackupCount % b.config.ObjectsToKeep
	b.currentBackupName = name
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
