package rethinkdb

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	connectionTimeout = 1 * time.Second

	rethinkDBDumpCmd    = "rethinkdb-dump"
	rethinkDBRestoreCmd = "rethinkdb-restore"
)

var (
	rethinkDBBackupFilePath  = filepath.Join(constants.BackupDir, "rethinkdb.tar.gz")
	rethinkDBRestoreFilePath = filepath.Join(constants.RestoreDir, "rethinkdb.tar.gz")
)

// RethinkDB implements the database interface
type RethinkDB struct {
	url          string
	passwordFile string
	log          *zap.SugaredLogger
	executor     *utils.CmdExecutor
}

// New instantiates a new rethinkdb database
func New(log *zap.SugaredLogger, url string, passwordFile string) *RethinkDB {
	return &RethinkDB{
		log:          log,
		url:          url,
		passwordFile: passwordFile,
		executor:     utils.NewExecutor(log),
	}
}

// Check checks whether a backup needs to be restored or not, returns true if it needs a backup
func (db *RethinkDB) Check() (bool, error) {
	empty, err := utils.IsEmpty(constants.DataDir)
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
		return errors.Wrap(err, "could not clean backup directory")
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return errors.Wrap(err, "could not create backup directory")
	}

	args := []string{"-f", rethinkDBBackupFilePath}
	if db.passwordFile != "" {
		args = append(args, "--password-file="+db.passwordFile)
	}
	if db.url != "" {
		args = append(args, "--connect="+db.url)
	}

	out, err := db.executor.ExecuteCommandWithOutput(rethinkDBDumpCmd, args...)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error running backup command: %s", out))
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

	args := []string{}
	if db.passwordFile != "" {
		args = append(args, "--password-file="+db.passwordFile)
	}
	if db.url != "" {
		args = append(args, "--connect="+db.url)
	}
	args = append(args, rethinkDBRestoreFilePath)

	out, err := db.executor.ExecuteCommandWithOutput(rethinkDBRestoreCmd, args...)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error running restore command: %s", out))
	}

	db.log.Infow("successfully restored rethinkdb database")

	return nil
}

// Probe indicates whether the database is running
func (db *RethinkDB) Probe() error {
	conn, err := net.DialTimeout("tcp", db.url, connectionTimeout)
	if err != nil {
		return fmt.Errorf("connection error: %v", err)
	}
	defer conn.Close()
	return nil
}

// StartForRestore indicates if the database needs to be started in order to restore it
func (db *RethinkDB) StartForRestore() bool {
	return true
}
