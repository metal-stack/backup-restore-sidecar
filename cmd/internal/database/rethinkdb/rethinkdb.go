package rethinkdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/probe"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"golang.org/x/sync/errgroup"

	r "gopkg.in/rethinkdb/rethinkdb-go.v6"
)

const (
	connectionTimeout             = 1 * time.Second
	restoreDatabaseStartupTimeout = 30 * time.Second

	rethinkDBCmd        = "rethinkdb"
	rethinkDBDumpCmd    = "rethinkdb-dump"
	rethinkDBRestoreCmd = "rethinkdb-restore"
)

var (
	rethinkDBBackupFilePath  = filepath.Join(constants.BackupDir, "rethinkdb")
	rethinkDBRestoreFilePath = filepath.Join(constants.RestoreDir, "rethinkdb")
)

// RethinkDB implements the database interface
type RethinkDB struct {
	datadir      string
	url          string
	passwordFile string
	log          *slog.Logger
	executor     *utils.CmdExecutor
}

// New instantiates a new rethinkdb database
func New(log *slog.Logger, datadir string, url string, passwordFile string) *RethinkDB {
	return &RethinkDB{
		log:          log,
		datadir:      datadir,
		url:          url,
		passwordFile: passwordFile,
		executor:     utils.NewExecutor(log),
	}
}

// Check indicates whether a restore of the database is required or not.
func (db *RethinkDB) Check(_ context.Context) (bool, error) {
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
func (db *RethinkDB) Backup(ctx context.Context) error {
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	args := []string{"-f", rethinkDBBackupFilePath}
	if db.passwordFile != "" {
		args = append(args, "--password-file="+db.passwordFile)
	}
	if db.url != "" {
		args = append(args, "--connect="+db.url)
	}

	out, err := db.executor.ExecuteCommandWithOutput(ctx, rethinkDBDumpCmd, nil, args...)
	fmt.Println(out)
	if err != nil {
		return fmt.Errorf("error running backup command: %w", err)
	}

	if strings.Contains(out, "0 rows exported from 0 tables, with 0 secondary indexes, and 0 hook functions") {
		return errors.New("the database is empty, taking a backup is not yet possible")
	}

	if _, err := os.Stat(rethinkDBBackupFilePath); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created: %s", rethinkDBBackupFilePath)
	}

	db.log.Debug("successfully took backup of rethinkdb database")

	return nil
}

// Recover restores a database backup
func (db *RethinkDB) Recover(ctx context.Context) error {
	if _, err := os.Stat(rethinkDBRestoreFilePath); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", rethinkDBRestoreFilePath)
	}

	passwordRaw, err := os.ReadFile(db.passwordFile)
	if err != nil {
		return fmt.Errorf("unable to read rethinkdb password file at %s: %w", db.passwordFile, err)
	}

	// rethinkdb requires to be running when restoring a backup.
	// however, if we let the real database container start, we cannot interrupt it anymore in case
	// an issue occurs during the restoration. therefore, we spin up an own instance of rethinkdb
	// inside the sidecar against which we can restore.

	var (
		cmd                           *exec.Cmd
		g, _                          = errgroup.WithContext(ctx)
		rethinkdbCtx, cancelRethinkdb = context.WithCancel(ctx) // cancel sends a KILL signal to the process

		// IMPORTANT: when the recovery goes wrong, the database directory MUST be cleaned up
		// otherwise on pod restart the database directory is not empty anymore and
		// the backup-restore-sidecar will assume it's a fresh database and let the
		// database start without restored data, which can mess up things big time

		handleFailedRecovery = func(restoreErr error) error {
			db.log.Error("trying to handle failed database recovery", "error", restoreErr)

			// kill the rethinkdb process
			cancelRethinkdb()

			db.log.Info("waiting for async rethinkdb go routine to stop")

			err := g.Wait()
			if err != nil {
				db.log.Error("rethinkdb go routine finished with error", "error", err)
			}

			if err := os.RemoveAll(db.datadir); err != nil {
				db.log.Error("unable to cleanup database data directory after failed recovery attempt, high risk of starting with fresh database on container restart", "err", err)
			} else {
				db.log.Info("cleaned up database data directory after failed recovery attempt to prevent start of fresh database")
			}

			return restoreErr
		}
	)

	defer cancelRethinkdb()

	g.Go(func() error {
		args := []string{"--bind", "all", "--driver-port", "1", "--directory", db.datadir, "--initial-password", strings.TrimSpace(string(passwordRaw))}
		db.log.Debug("execute rethinkdb", "args", args)

		cmd = exec.CommandContext(rethinkdbCtx, rethinkDBCmd, args...) // nolint:gosec
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("unable to run rethinkdb: %w", err)
		}

		db.log.Info("rethinkdb process finished")

		return nil
	})

	db.log.Info("waiting for rethinkdb database to come up")

	probeCtx, probeCancel := context.WithTimeout(ctx, restoreDatabaseStartupTimeout)
	defer probeCancel()

	restoreDB := New(db.log, db.datadir, "localhost:1", db.passwordFile)
	err = probe.Start(probeCtx, restoreDB.log, restoreDB)
	if err != nil {
		return handleFailedRecovery(fmt.Errorf("rethinkdb did not come up: %w", err))
	}

	args := []string{}
	if db.url != "" {
		args = append(args, "--connect="+restoreDB.url)
	}
	if db.passwordFile != "" {
		args = append(args, "--password-file="+db.passwordFile)
	}
	args = append(args, rethinkDBRestoreFilePath)

	out, err := db.executor.ExecuteCommandWithOutput(ctx, rethinkDBRestoreCmd, nil, args...)
	fmt.Println(out)
	if err != nil {
		return handleFailedRecovery(fmt.Errorf("error running restore command: %w", err))
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		db.log.Error("failed to send sigterm signal to rethinkdb, killing it", "error", err)
		cancelRethinkdb()
	}

	err = g.Wait()
	if err != nil {
		db.log.Error("rethinkdb process not properly terminated, but restore was successful", "error", err)
	} else {
		db.log.Info("successfully restored rethinkdb database")
	}

	return nil
}

// Probe figures out if the database is running and available for taking backups.
func (db *RethinkDB) Probe(ctx context.Context) error {
	passwordRaw, err := os.ReadFile(db.passwordFile)
	if err != nil {
		return fmt.Errorf("unable to read rethinkdb password file at %s: %w", db.passwordFile, err)
	}

	session, err := r.Connect(r.ConnectOpts{
		Addresses: []string{db.url},
		Username:  "admin",
		Password:  strings.TrimSpace(string(passwordRaw)),
		MaxIdle:   10,
		MaxOpen:   20,
	})
	if err != nil {
		return fmt.Errorf("cannot create rethinkdb client: %w", err)
	}

	_, err = r.DB("rethinkdb").Table("server_status").Run(session)
	if err != nil {
		return fmt.Errorf("error retrieving rethinkdb server status: %w", err)
	}

	return nil
}

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
func (db *RethinkDB) Upgrade(_ context.Context) error {
	return nil
}
