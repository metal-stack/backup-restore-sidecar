package database

import "context"

type DatabaseInitializer interface {
	// Check indicates whether a restore of the database is required or not.
	Check(ctx context.Context) (bool, error)

	// Recover performs a restore of the database.
	Recover(ctx context.Context) error

	// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
	//
	// The function aborts the update without returning an error as long as the old data stays unmodified and only logs the error.
	// This behavior is intended to reduce unnecessary downtime caused by misconfigurations.
	//
	// Once the upgrade was made, any error condition will require to recover the database from backup.
	Upgrade(ctx context.Context) error
}

type DatabaseProber interface {
	// Probe figures out if the database is running and available for taking backups.
	Probe(ctx context.Context) error

	// Backup creates a backup of the database.
	Backup(ctx context.Context) error
}

type Database interface {
	DatabaseInitializer
	DatabaseProber
}

// DatabaseLeaderElector is an optional interface that databases can implement
// to indicate whether they should perform backups based on leader election.
// If not implemented, the database will always perform backups.
type DatabaseLeaderElector interface {
	// ShouldPerformBackup returns true if this instance should perform backups.
	// For clustered databases with leader election, only the leader should return true.
	ShouldPerformBackup(ctx context.Context) bool
}
