package postgres

import (
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	connectionTimeout = 1 * time.Second

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

// Check checks whether a backup needs to be restored or not, returns true if it needs a backup
func (db *Postgres) Check() (bool, error) {
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
func (db *Postgres) Backup() error {
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return errors.Wrap(err, "could not clean backup directory")
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return errors.Wrap(err, "could not create backup directory")
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

	out, err := db.executor.ExecuteCommandWithOutput(postgresBackupCmd, env, args...)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error running backup command: %s", out))
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
func (db *Postgres) Recover() error {
	for _, p := range []string{postgresBaseTar, postgresWalTar} {
		fullPath := path.Join(constants.RestoreDir, p)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return fmt.Errorf("restore file not present: %s", fullPath)
		}
	}

	if err := utils.RemoveContents(db.datadir); err != nil {
		return errors.Wrap(err, "could not clean database data directory")
	}

	out, err := db.executor.ExecuteCommandWithOutput("tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresBaseTar), "-C", db.datadir)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error untaring base backup: %s", out))
	}

	db.log.Debugw("restored postgres base backup", "output", out)

	if err := os.RemoveAll(path.Join(db.datadir, "pg_wal")); err != nil {
		return errors.Wrap(err, "could not clean pg_wal directory")
	}

	if err := os.MkdirAll(path.Join(db.datadir, "pg_wal"), 0777); err != nil {
		return errors.Wrap(err, "could not create pg_wal directory")
	}

	out, err = db.executor.ExecuteCommandWithOutput("tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresWalTar), "-C", path.Join(db.datadir, "pg_wal"))
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error untaring wal backup: %s", out))
	}

	db.log.Debugw("restored postgres pg_wal backup", "output", out)

	db.log.Infow("successfully restored postgres database")

	return nil
}

// Probe indicates whether the database is running
func (db *Postgres) Probe() error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(db.host, strconv.Itoa(db.port)), connectionTimeout)
	if err != nil {
		return fmt.Errorf("connection error:%w", err)
	}
	defer conn.Close()
	return nil
}
