package providers

import "time"

type BackupProvider interface {
	EnsureBackupBucket() error
	ListBackups() (BackupVersions, error)
	CleanupBackups() error
	DownloadBackup(version *BackupVersion) error
	UploadBackup(sourcePath string) error
}

type BackupVersions interface {
	Latest() *BackupVersion
	Sort(versions []*BackupVersion, asc bool)
	List() []*BackupVersion
}

type BackupVersion struct {
	Name    string
	Version string
	Date    time.Time
}
