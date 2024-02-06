package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"

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
	log      *slog.Logger
	executor *utils.CmdExecutor
}

// New instantiates a new postgres database
func New(log *slog.Logger, datadir string, host string, port int, user string, password string) *Postgres {
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

	db.log.Debug("successfully took backup of postgres database", "output", out)

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

	db.log.Debug("restored postgres base backup", "output", out)

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

	db.log.Debug("restored postgres pg_wal backup", "output", out)

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
		db.log.Info("detected running timescaledb, running post-start hook to update timescaledb extension if necessary")

		err = db.updateTimescaleDB(ctx, dbc)
		if err != nil {
			return fmt.Errorf("unable to update timescaledb: %w", err)
		}
	}

	return nil
}

func (db *Postgres) updateTimescaleDB(ctx context.Context, dbc *sql.DB) error {
	var (
		databaseNames []string
	)

	databaseNameRows, err := dbc.QueryContext(ctx, "SELECT datname,datallowconn FROM pg_database")
	if err != nil {
		return fmt.Errorf("unable to get database names: %w", err)
	}
	defer databaseNameRows.Close()

	for databaseNameRows.Next() {
		var name string
		var allowed bool
		if err := databaseNameRows.Scan(&name, &allowed); err != nil {
			return err
		}

		if allowed {
			databaseNames = append(databaseNames, name)
		}
	}
	if err := databaseNameRows.Err(); err != nil {
		return err
	}

	for _, dbName := range databaseNames {
		connString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", db.host, db.port, db.user, db.password, dbName)
		dbc2, err := sql.Open("postgres", connString)
		if err != nil {
			return fmt.Errorf("unable to open postgres connection %w", err)
		}
		defer dbc2.Close()

		rows, err := dbc2.QueryContext(ctx, "SELECT extname FROM pg_extension")
		if err != nil {
			return fmt.Errorf("unable to get extensions: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var extName string
			if err := rows.Scan(&extName); err != nil {
				return err
			}

			if extName != "timescaledb" {
				continue
			}

			db.log.Info("updating timescaledb extension", "db-name", dbName)

			_, err = dbc2.ExecContext(ctx, "ALTER EXTENSION timescaledb UPDATE")
			if err != nil {
				return fmt.Errorf("unable to update extension: %w", err)
			}

			break
		}

		if err := rows.Err(); err != nil {
			return err
		}
	}

	return nil
}
