package database

type DatabaseInitializer interface {
	Check() (bool, error)
	Recover() error
	Upgrade() error
}

type DatabaseProber interface {
	Probe() error
	Backup() error
}

type Database interface {
	DatabaseInitializer
	DatabaseProber
}
