package providers

import (
	"context"
	"io"
	"time"
)

type BackupProvider interface {
	EnsureBackupBucket(ctx context.Context) error
	ListBackups(ctx context.Context) (BackupVersions, error)
	CleanupBackups(ctx context.Context) error
	GetNextBackupName(ctx context.Context) string
	DownloadBackup(ctx context.Context, version *BackupVersion, writer io.Writer) error
	UploadBackup(ctx context.Context, reader io.Reader) error
}

type BackupVersions interface {
	// Latest returns the most recent backup
	Latest() *BackupVersion
	// List returns all backups sorted by date descending, e.g. the newest backup comes first
	List() []*BackupVersion
	// Get a backup at the specified version
	Get(version string) (*BackupVersion, error)
}

type BackupVersion struct {
	Name    string
	Version string
	Date    time.Time
}
