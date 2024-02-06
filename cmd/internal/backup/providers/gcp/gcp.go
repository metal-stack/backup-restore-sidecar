package gcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"cloud.google.com/go/storage"
)

const (
	defaultBackupName = "db"
)

// BackupProviderGCP implements the backup provider interface for GCP
type BackupProviderGCP struct {
	fs     afero.Fs
	log    *slog.Logger
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
	FS             afero.Fs
	ClientOpts     []option.ClientOption
}

func (c *BackupProviderConfigGCP) validate() error {
	if c.BucketName == "" {
		return errors.New("gcp bucket name must not be empty")
	}
	if c.ProjectID == "" {
		return errors.New("gcp project id must not be empty")
	}
	for _, opt := range c.ClientOpts {
		if opt == nil {
			return errors.New("option can not be nil")
		}
	}

	return nil
}

// New returns a GCP backup provider
func New(ctx context.Context, log *slog.Logger, config *BackupProviderConfigGCP) (*BackupProviderGCP, error) {
	if config == nil {
		return nil, errors.New("gcp backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.BackupName == "" {
		config.BackupName = defaultBackupName
	}
	if config.FS == nil {
		config.FS = afero.NewOsFs()
	}

	err := config.validate()
	if err != nil {
		return nil, err
	}

	client, err := storage.NewClient(ctx, config.ClientOpts...)
	if err != nil {
		return nil, err
	}

	return &BackupProviderGCP{
		c:      client,
		config: config,
		log:    log,
		fs:     config.FS,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderGCP) EnsureBackupBucket(ctx context.Context) error {
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
		var googleErr *googleapi.Error
		if errors.As(err, &googleErr) {
			if googleErr.Code != http.StatusConflict {
				return err
			}
		} else {
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
func (b *BackupProviderGCP) CleanupBackups(_ context.Context) error {
	// nothing to do here, done with lifecycle rules
	return nil
}

// DownloadBackup downloads the given backup version to the restoration folder
func (b *BackupProviderGCP) DownloadBackup(ctx context.Context, version *providers.BackupVersion) error {
	gen, err := strconv.ParseInt(version.Version, 10, 64)
	if err != nil {
		return err
	}

	bucket := b.c.Bucket(b.config.BucketName)

	downloadFileName := version.Name
	if strings.Contains(downloadFileName, "/") {
		downloadFileName = filepath.Base(downloadFileName)
	}
	backupFilePath := path.Join(constants.DownloadDir, downloadFileName)

	b.log.Info("downloading", "object", version.Name, "gen", gen, "to", backupFilePath)

	r, err := bucket.Object(version.Name).Generation(gen).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}
	defer r.Close()

	f, err := b.fs.Create(backupFilePath)
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
func (b *BackupProviderGCP) UploadBackup(ctx context.Context, sourcePath string) error {
	bucket := b.c.Bucket(b.config.BucketName)

	r, err := b.fs.Open(sourcePath)
	if err != nil {
		return err
	}
	defer r.Close()

	destination := filepath.Base(sourcePath)
	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debug("uploading object", "src", sourcePath, "dest", destination)

	obj := bucket.Object(destination)
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	defer w.Close()

	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderGCP) GetNextBackupName(_ context.Context) string {
	// name is constant because we use lifecycle rule to cleanup
	return b.config.BackupName
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderGCP) ListBackups(ctx context.Context) (providers.BackupVersions, error) {
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
