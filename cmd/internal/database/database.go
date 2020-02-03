package database

type DatabaseInitializer interface {
	Check() (bool, error)
	Recover() error
	StartForRestore() bool
}

type DatabaseProber interface {
	Probe() error
	Backup() error
}

type Database interface {
	DatabaseInitializer
	DatabaseProber
}
