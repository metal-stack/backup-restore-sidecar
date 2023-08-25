package constants

const (
	// DefaultObjectsToKeep are the default number of objects to keep at the cloud provider bucket
	DefaultObjectsToKeep = 20

	SidecarBaseDir = "/backup-restore-sidecar"

	// BackupDir is the directory in the sidecar where the database backup contents to be archived live in
	BackupDir = SidecarBaseDir + "/backup/files"
	// UploadDir is the path where the backup files are archived in and uploaded to the backup provider
	UploadDir = SidecarBaseDir + "/backup"
	// RestoreDir is the directory in the sidecar where the database backup contents will be unarchived to
	RestoreDir = SidecarBaseDir + "/restore/files"
	// DownloadDir is the path where the backup archive will be downloaded to before it is being unarchived to the restore dir
	DownloadDir = SidecarBaseDir + "/restore"
)
