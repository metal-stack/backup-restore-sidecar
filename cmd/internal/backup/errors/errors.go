package errors

// NoBackupsAvailableError indicates that no backups for the database exist in the cloud provider bucket
type NoBackupsAvailableError struct{}

func (e NoBackupsAvailableError) Error() string {
	return "no backups available"
}
