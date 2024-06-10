package providers

import (
	"context"
	"time"
)

type BackupProvider interface {
	EnsureBackupBucket(ctx context.Context) error
	ListBackups(ctx context.Context) (BackupVersions, error)
	CleanupBackups(ctx context.Context) error
	GetNextBackupName(ctx context.Context) string
	DownloadBackup(ctx context.Context, version *BackupVersion) error
	UploadBackup(ctx context.Context, sourcePath string) error
}

type BackupVersions interface {
	Latest() *BackupVersion
	List() []*BackupVersion
	Get(version string) (*BackupVersion, error)
}

type BackupVersion struct {
	Name    string
	Version string
	Date    time.Time
}
