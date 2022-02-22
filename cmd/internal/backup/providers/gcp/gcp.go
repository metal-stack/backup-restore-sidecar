package gcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"

	"go.uber.org/zap"
	"google.golang.org/api/iterator"

	"cloud.google.com/go/storage"
)

const (
	defaultBackupName = "db"
)

// BackupProviderGCP implements the backup provider interface for GCP
type BackupProviderGCP struct {
	log    *zap.SugaredLogger
	c      *storage.Client
	config *BackupProviderConfigGCP
}

// BackupProviderConfigGCP provides configuration for the BackupProviderGCP
type BackupProviderConfigGCP struct {
	BucketName     string
	BucketLocation string
	BackupName     string
	ObjectPrefix   string
	ObjectsToKeep  int64
	ProjectID      string
}

func (c *BackupProviderConfigGCP) validate() error {
	if c.BucketName == "" {
		return errors.New("gcp bucket name must not be empty")
	}
	if c.ProjectID == "" {
		return errors.New("gcp project id must not be empty")
	}

	return nil
}

// New returns a GCP backup provider
func New(log *zap.SugaredLogger, config *BackupProviderConfigGCP) (*BackupProviderGCP, error) {
	ctx := context.Background()

	if config == nil {
		return nil, errors.New("gcp backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.BackupName == "" {
		config.BackupName = defaultBackupName
	}

	err := config.validate()
	if err != nil {
		return nil, err
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &BackupProviderGCP{
		c:      client,
		config: config,
		log:    log,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderGCP) EnsureBackupBucket() error {
	ctx := context.Background()

	bucket := b.c.Bucket(b.config.BucketName)
	lifecycle := storage.Lifecycle{
		Rules: []storage.LifecycleRule{
			{
				Condition: storage.LifecycleCondition{
					NumNewerVersions: b.config.ObjectsToKeep,
				},
				Action: storage.LifecycleAction{
					Type: "Delete",
				},
			},
		},
	}
	attrs := &storage.BucketAttrs{
		Location:          b.config.BucketLocation,
		VersioningEnabled: true,
		Lifecycle:         lifecycle,
	}

	if err := bucket.Create(ctx, b.config.ProjectID, attrs); err != nil {
		if !strings.Contains(err.Error(), "you already own it") {
			return err
		}
	}

	attrsToUpdate := storage.BucketAttrsToUpdate{
		VersioningEnabled: true,
		Lifecycle:         &lifecycle,
	}
	if _, err := bucket.Update(ctx, attrsToUpdate); err != nil {
		return err
	}

	return nil
}

// CleanupBackups cleans up backups according to the given backup cleanup policy at the backup provider
func (b *BackupProviderGCP) CleanupBackups() error {
	// nothing to do here, done with lifecycle rules
	return nil
}

// DownloadBackup downloads the given backup version to the restoration folder
func (b *BackupProviderGCP) DownloadBackup(version *providers.BackupVersion) error {
	gen, err := strconv.ParseInt(version.Version, 10, 64)
	if err != nil {
		return err
	}

	ctx := context.Background()

	bucket := b.c.Bucket(b.config.BucketName)

	r, err := bucket.Object(version.Name).Generation(gen).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}
	defer r.Close()

	downloadFileName := version.Name
	if strings.Contains(downloadFileName, "/") {
		downloadFileName = filepath.Base(downloadFileName)
	}
	backupFilePath := path.Join(constants.DownloadDir, downloadFileName)
	f, err := os.Create(backupFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	if err != nil {
		return fmt.Errorf("error writing file from gcp to filesystem: %w", err)
	}

	return nil
}

// UploadBackup uploads a backup to the backup provider
func (b *BackupProviderGCP) UploadBackup(sourcePath string) error {
	ctx := context.Background()
	bucket := b.c.Bucket(b.config.BucketName)

	r, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer r.Close()

	destination := filepath.Base(sourcePath)
	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debugw("uploading object", "src", sourcePath, "dest", destination)

	obj := bucket.Object(destination)
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	defer w.Close()

	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderGCP) GetNextBackupName() string {
	// name is constant because we use lifecycle rule to cleanup
	return b.config.BackupName
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderGCP) ListBackups() (providers.BackupVersions, error) {
	ctx := context.Background()

	bucket := b.c.Bucket(b.config.BucketName)

	query := &storage.Query{
		Versions: true,
	}
	if b.config.ObjectPrefix != "" {
		query.Prefix = b.config.ObjectPrefix
	}
	it := bucket.Objects(ctx, query)

	var objectAttrs []*storage.ObjectAttrs
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}

		objectAttrs = append(objectAttrs, attrs)
	}

	return BackupVersionsGCP{
		objectAttrs: objectAttrs,
	}, nil
}
