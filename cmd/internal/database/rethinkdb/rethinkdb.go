package rethinkdb

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/probe"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"go.uber.org/zap"
)

const (
	connectionTimeout              = 1 * time.Second
	restoreDatabaseStartupTimeout  = 30 * time.Second
	restoreDatabaseShutdownTimeout = 10 * time.Second

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
	log          *zap.SugaredLogger
	executor     *utils.CmdExecutor
}

// New instantiates a new rethinkdb database
func New(log *zap.SugaredLogger, datadir string, url string, passwordFile string) *RethinkDB {
	return &RethinkDB{
		log:          log,
		datadir:      datadir,
		url:          url,
		passwordFile: passwordFile,
		executor:     utils.NewExecutor(log),
	}
}

// Check checks whether a backup needs to be restored or not, returns true if it needs a backup
func (db *RethinkDB) Check() (bool, error) {
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
func (db *RethinkDB) Backup() error {
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

	out, err := db.executor.ExecuteCommandWithOutput(rethinkDBDumpCmd, nil, args...)
	if err != nil {
		return fmt.Errorf("error running backup command: %s %w", out, err)
	}

	if strings.Contains(out, "0 rows exported from 0 tables, with 0 secondary indexes, and 0 hook functions") {
		return errors.New("the database is empty, taking a backup is not yet possible")
	}

	if _, err := os.Stat(rethinkDBBackupFilePath); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created: %s", rethinkDBBackupFilePath)
	}

	db.log.Debugw("successfully took backup of rethinkdb database", "output", out)

	return nil
}

// Recover restores a database backup
func (db *RethinkDB) Recover() error {
	if _, err := os.Stat(rethinkDBRestoreFilePath); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", rethinkDBRestoreFilePath)
	}

	// rethinkdb requires to be running when restoring a backup.
	// however, if we let the real database container start, we cannot interrupt it anymore in case
	// an issue occurs during the restoration. therefore, we spin up an own instance of rethinkdb
	// inside the sidecar against which we can restore.

	db.log.Infow("starting rethinkdb database within sidecar for restore")
	//nolint
	cmd := exec.Command(rethinkDBCmd, "--bind", "all", "--driver-port", "1", "--directory", db.datadir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("unable to start database within sidecar for restore: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	db.log.Infow("waiting for rethinkdb database to come up")
	restoreDB := New(db.log, db.datadir, "localhost:1", "")
	stop := make(chan struct{})
	done := make(chan bool)
	defer close(done)
	var err error
	go func() {
		err = probe.Start(restoreDB.log, restoreDB, stop)
		done <- true
	}()
	select {
	case <-done:
		if err != nil {
			return fmt.Errorf("error while probing: %w", err)
		}
		db.log.Infow("rethinkdb in sidecar is now available, now triggering restore commands...")
	case <-time.After(restoreDatabaseStartupTimeout):
		close(stop)
		return errors.New("rethinkdb database did not come up in time")
	}

	args := []string{}
	if db.url != "" {
		args = append(args, "--connect="+restoreDB.url)
	}
	args = append(args, rethinkDBRestoreFilePath)

	out, err := db.executor.ExecuteCommandWithOutput(rethinkDBRestoreCmd, nil, args...)
	if err != nil {
		return fmt.Errorf("error running restore command: %s %w", out, err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send sigterm signal to rethinkdb: %w", err)
	}

	wait := make(chan error)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			return fmt.Errorf("rethinkdb did not shutdown cleanly: %w", err)
		}
		db.log.Infow("successfully restored rethinkdb database", "output", out)
	case <-time.After(restoreDatabaseShutdownTimeout):
		return fmt.Errorf("rethinkdb did not shutdown cleanly after %s", restoreDatabaseShutdownTimeout)
	}

	return nil
}

// Probe indicates whether the database is running
func (db *RethinkDB) Probe() error {
	conn, err := net.DialTimeout("tcp", db.url, connectionTimeout)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer conn.Close()
	return nil
}
