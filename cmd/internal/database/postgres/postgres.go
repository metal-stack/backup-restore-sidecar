package postgres

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"go.uber.org/zap"
)

const (
	connectionTimeout = 1 * time.Second

	postgresBackupCmd = "pg_basebackup"
	postgresBaseTar   = "base.tar.gz"
	postgresWalTar    = "pg_wal.tar.gz"

	postgresConfigCmd   = "pg_config"
	postgresVersionFile = "PG_VERSION"
	oldPostgresBinDir   = "/usr/local/bin/pg-old"
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

	out, err := db.executor.ExecuteCommandWithOutput(postgresBackupCmd, env, args...)
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
func (db *Postgres) Recover() error {
	for _, p := range []string{postgresBaseTar, postgresWalTar} {
		fullPath := path.Join(constants.RestoreDir, p)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return fmt.Errorf("restore file not present: %s", fullPath)
		}
	}

	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	out, err := db.executor.ExecuteCommandWithOutput("tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresBaseTar), "-C", db.datadir)
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

	out, err = db.executor.ExecuteCommandWithOutput("tar", nil, "-xzvf", path.Join(constants.RestoreDir, postgresWalTar), "-C", path.Join(db.datadir, "pg_wal"))
	if err != nil {
		return fmt.Errorf("error untaring wal backup: %s %w", out, err)
	}

	db.log.Debugw("restored postgres pg_wal backup", "output", out)

	db.log.Info("successfully restored postgres database")

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

// Upgrade indicates whether the database files are from a previous version of and need to be upgraded
func (db *Postgres) Upgrade() error {
	// First check if there are data already present
	pgVersionFile := path.Join(db.datadir, postgresVersionFile)
	if _, err := os.Stat(pgVersionFile); errors.Is(err, fs.ErrNotExist) {
		db.log.Infow("PG_VERSION is not present, no upgrade required")
		return nil
	}

	// Then check the version of the existing database
	// cat PG_VERSION
	// 12
	pgVersionBytes, err := os.ReadFile(pgVersionFile)
	if err != nil {
		db.log.Infow("unable to read PG_VERSION", "error", err)
		return nil
	}
	pgVersion, err := strconv.Atoi(strings.TrimSpace(string(pgVersionBytes)))
	if err != nil {
		db.log.Infow("unable to parse PG_VERSION to an int", "PG_VERSION", string(pgVersionBytes), "error", err)
		return nil
	}

	// Now check the version of the postgres binaries
	// pg_config  --version
	// PostgreSQL 12.16
	binaryVersionMajor, err := db.getBinaryVersion(postgresConfigCmd)
	if err != nil {
		db.log.Infow("unable to get binary version", "error", err)
		return nil
	}

	if pgVersion == binaryVersionMajor {
		db.log.Infow("no version difference, skipping upgrade", "database version", pgVersion, "binary version", binaryVersionMajor)
		return nil
	}
	if pgVersion > binaryVersionMajor {
		db.log.Infow("database is newer than postgres binary, abort", "database version", pgVersion, "binary version", binaryVersionMajor)
		return fmt.Errorf("database is newer than postgres binary")
	}

	// Check if old pg binaries are present and match pgVersion
	oldPGConfigCmd := path.Join(oldPostgresBinDir, postgresConfigCmd)
	if _, err := os.Stat(oldPGConfigCmd); errors.Is(err, fs.ErrNotExist) {
		db.log.Infow("pg_config of old version not present, skipping upgrade")
		return nil
	}

	oldBinaryVersionMajor, err := db.getBinaryVersion(oldPGConfigCmd)
	if err != nil {
		db.log.Infow("unable to get old binary version", "error", err)
		return nil
	}

	// OK we need to upgrade the database in place, maybe taking a backup before is recommended
	db.log.Infow("start upgrading from", "old database", pgVersion, "old binary", oldBinaryVersionMajor, "new binary", binaryVersionMajor)

	return nil
}

func (db *Postgres) getBinaryVersion(pgConfigCmd string) (int, error) {
	cmd := exec.Command(pgConfigCmd, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("unable to detect postgres binary version, skipping upgrade %w", err)
	}
	_, binaryVersionString, found := strings.Cut(string(out), "PostgreSQL ")
	if !found {
		return 0, fmt.Errorf("unable to detect postgres binary version in pg_config output, skipping upgrade, output:%q", binaryVersionString)
	}
	binaryVersionMajorString, _, found := strings.Cut(binaryVersionString, ".")
	if !found {
		return 0, fmt.Errorf("unable to parse postgres binary version, skipping upgrade")
	}
	binaryVersionMajor, err := strconv.Atoi(binaryVersionMajorString)
	if err != nil {
		return 0, fmt.Errorf("unable to parse postgres binary version to an int, skipping upgrade %w", err)
	}
	return binaryVersionMajor, nil
}
