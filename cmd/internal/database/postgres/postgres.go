package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"strconv"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"go.uber.org/zap"

	_ "github.com/lib/pq"
)

const (
	postgresBackupCmd = "pg_basebackup"
	postgresBaseTar   = "base.tar.gz"
	postgresWalTar    = "pg_wal.tar.gz"
)

// Postgres implements the database interface
type Postgres struct {
	datadir  string
	host     string
	port     int
	user     string
	password string
	log      *zap.SugaredLogger
	executor *utils.CmdExecutor
}

// New instantiates a new postgres database
func New(log *zap.SugaredLogger, datadir string, host string, port int, user string, password string) *Postgres {
	return &Postgres{
		log:      log,
		datadir:  datadir,
		host:     host,
		port:     port,
		user:     user,
		password: password,
		executor: utils.NewExecutor(log),
	}
}

// Check indicates whether a restore of the database is required or not.
func (db *Postgres) Check(_ context.Context) (bool, error) {
	empty, err := utils.IsEmpty(db.datadir)
	if err != nil {
		return false, err
	}
	if empty {
		db.log.Info("data directory is empty")
		return true, err
	}

	return false, nil
}

// Backup takes a backup of the database
func (db *Postgres) Backup(ctx context.Context) error {
	// for new databases the postgres binaries required for Upgrade() cannot be copied before the database is running
	// therefore this happens in the backup task where the database is already available
	//
	// implication: one backup has to be taken before an upgrade can be made
	err := db.copyPostgresBinaries(ctx, false)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	args := []string{"-D", constants.BackupDir, "--wal-method=stream", "--checkpoint=fast", "-z", "--format=t"}
	if db.host != "" {
		args = append(args, "--host="+db.host)
	}
	if db.port != 0 {
		args = append(args, "--port="+strconv.Itoa(db.port))
	}
	if db.user != "" {
		args = append(args, "--username="+db.user)
	}

	var env []string
	if db.password != "" {
		env = append(env, "PGPASSWORD="+db.password)
	}

	out, err := db.executor.ExecuteCommandWithOutput(ctx, postgresBackupCmd, env, args...)
	if err != nil {
		return fmt.Errorf("error running backup command: %s %w", out, err)
	}

	for _, p := range []string{postgresBaseTar, postgresWalTar} {
		fullPath := path.Join(constants.BackupDir, p)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return fmt.Errorf("backup file was not created: %s", fullPath)
		}
	}

	db.log.Debugw("successfully took backup of postgres database", "output", out)

	return nil
}

// Recover restores a database backup
func (db *Postgres) Recover(ctx context.Context) error {
	for _, p := range []string{postgresBaseTar, postgresWalTar} {
		fullPath := path.Join(constants.RestoreDir, p)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return fmt.Errorf("restore file not present: %s", fullPath)
		}
	}

	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	out, err := db.executor.ExecuteCommandWithOutput(ctx, "tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresBaseTar), "-C", db.datadir)
	if err != nil {
		return fmt.Errorf("error untaring base backup: %s %w", out, err)
	}

	db.log.Debugw("restored postgres base backup", "output", out)

	if err := os.RemoveAll(path.Join(db.datadir, "pg_wal")); err != nil {
		return fmt.Errorf("could not clean pg_wal directory: %w", err)
	}

	if err := os.MkdirAll(path.Join(db.datadir, "pg_wal"), 0777); err != nil {
		return fmt.Errorf("could not create pg_wal directory: %w", err)
	}

	out, err = db.executor.ExecuteCommandWithOutput(ctx, "tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresWalTar), "-C", path.Join(db.datadir, "pg_wal"))
	if err != nil {
		return fmt.Errorf("error untaring wal backup: %s %w", out, err)
	}

	db.log.Debugw("restored postgres pg_wal backup", "output", out)

	db.log.Info("successfully restored postgres database")

	return nil
}

// Probe figures out if the database is running and available for taking backups.
func (db *Postgres) Probe(ctx context.Context) error {
	// TODO is postgres db OK ?
	connString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", db.host, db.port, db.user, db.password)

	dbc, err := sql.Open("postgres", connString)
	if err != nil {
		return fmt.Errorf("unable to open postgres connection %w", err)
	}
	defer dbc.Close()

	err = dbc.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("unable to ping postgres connection %w", err)
	}

	runsTimescaleDB, err := db.runningTimescaleDB(ctx, postgresConfigCmd)
	if err == nil && runsTimescaleDB {
		db.log.Infow("detected running timescaledb, running post-start hook to update timescaledb extension if necessary")

		_, err = dbc.ExecContext(ctx, "ALTER EXTENSION timescaledb UPDATE;")
		if err != nil {
			return fmt.Errorf("unable to alter extension: %w", err)
		}

		// we also need to upgrade the extension in the template1 database because there it is also installed

		connString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=template1 sslmode=disable", db.host, db.port, db.user, db.password)
		dbcTemplate1, err := sql.Open("postgres", connString)
		if err != nil {
			return fmt.Errorf("unable to open postgres connection %w", err)
		}
		defer dbcTemplate1.Close()

		_, err = dbcTemplate1.ExecContext(ctx, "ALTER EXTENSION timescaledb UPDATE;")
		if err != nil {
			return fmt.Errorf("unable to alter extension for template database: %w", err)
		}
	}

	return nil
}
