package providers

import "time"

type BackupProvider interface {
	EnsureBackupBucket() error
	ListBackups() (BackupVersions, error)
	CleanupBackups() error
	GetNextBackupName() string
	DownloadBackup(version *BackupVersion) error
	UploadBackup(sourcePath string) error
}

type BackupVersions interface {
	Latest() *BackupVersion
	Sort(versions []*BackupVersion, asc bool)
	List() []*BackupVersion
	Get(version string) (*BackupVersion, error)
}

type BackupVersion struct {
	Name    string
	Version string
	Date    time.Time
}
