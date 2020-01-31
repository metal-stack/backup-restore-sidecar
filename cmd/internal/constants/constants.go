package constants

const (
	// DefaultObjectsToKeep are the default number of objects to keep at the cloud provider bucket
	DefaultObjectsToKeep = 20

	// BackupDir is the directory in the sidecar where the database backup contents to be archived live in
	BackupDir = "/tmp/backup-restore-sidecar/backup/files"
	// UploadDir is the path where the backup files are archived in and uploaded to the backup provider
	UploadDir = "/tmp/backup-restore-sidecar/backup"
	// RestoreDir is the directory in the sidecar where the database backup contents will be unarchived to
	RestoreDir = "/tmp/backup-restore-sidecar/restore/files"
	// DownloadDir is the path where the backup archive will be downloaded to before it is being unarchived to the restore dir
	DownloadDir = "/tmp/backup-restore-sidecar/restore"
	// DataDir is the directory in the sidecar where the database stores its data and where the backup can be restored in
	DataDir = "/data"
)
