package constants

import "errors"

var (
	ErrBackupAlreadyInProgress = errors.New("a backup is already in progress")
)
