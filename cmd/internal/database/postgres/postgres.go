package postgres

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
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
	postgresUpgradeCmd  = "pg_upgrade"
	postgresInitDBCmd   = "initdb"
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

	// Check if pg_upgrade is present
	p, err := exec.LookPath(postgresUpgradeCmd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		db.log.Infow("pg_upgrade is not present, skipping upgrade")
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

	if oldBinaryVersionMajor != pgVersion {
		db.log.Infow("database version and old binary version do not match, skipping upgrade", "old database", pgVersion, "old binary", oldBinaryVersionMajor)
		return nil
	}

	// OK we need to upgrade the database in place, maybe taking a backup before is recommended
	db.log.Infow("start upgrading from", "old database", pgVersion, "old binary", oldBinaryVersionMajor, "new binary", binaryVersionMajor)

	// Take a backup
	// masterdata-db-0 backup-restore-sidecar {"level":"info","timestamp":"2023-08-20T12:10:44Z","logger":"postgres","caller":"postgres/postgres.go:240","msg":"creating a backup before upgrading failed, skipping upgrade","error":"error running backup command: pg_basebackup: error: connection to server at \"127.0.0.1\", port
	// 5432 failed: Connection refused\n\tIs the server running on that host and accepting TCP/IP connections? exit status 1"}
	// err = db.Backup()
	// if err != nil {
	// 	db.log.Infow("creating a backup before upgrading failed, skipping upgrade", "error", err)
	// 	return nil
	// }

	// run the pg_upgrade command as postgres user
	pgUser, err := user.Lookup("postgres")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(pgUser.Uid)
	if err != nil {
		return err
	}
	err = syscall.Setuid(uid)
	if err != nil {
		return err
	}

	// remove /data/postgres-new if present
	newDataDirTemp := path.Join("/data", "postgres-new")
	err = os.RemoveAll(newDataDirTemp)
	if err != nil {
		db.log.Infow("unable to remove new datadir, skipping upgrade", "error", err)
		return nil
	}

	// initdb -D /data/postgres-new
	cmd := exec.Command(postgresInitDBCmd, "-D", newDataDirTemp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	out, err := cmd.CombinedOutput()
	if err != nil {
		db.log.Infow("unable to run initdb on new new datadir, skipping upgrade", "error", err)
		return nil
	}
	db.log.Infow("new database directory initialized", "output", string(out))

	// restore old pg_hba.conf
	pgHBAConf, err := os.ReadFile(path.Join(db.datadir, "pg_hba.conf"))
	if err != nil {
		return err
	}
	err = os.WriteFile(path.Join(newDataDirTemp, "pg_hba.conf"), pgHBAConf, 0600)
	if err != nil {
		return err
	}

	// pg_upgrade \
	// --old-datadir /data/postgres \
	// --new-datadir /data/postgres-new \
	// --old-bindir /usr/local/bin/pg-old \
	// --new-bindir /usr/local/bin \
	// --link
	pgUpgradeArgs := []string{
		"--old-datadir", db.datadir,
		"--new-datadir", newDataDirTemp,
		"--old-bindir", oldPostgresBinDir,
		"--new-bindir", "/usr/local/bin",
		"--link",
	}
	db.log.Infow("running pg_upgrade with", "args", pgUpgradeArgs)
	cmd = exec.Command(postgresUpgradeCmd, pgUpgradeArgs...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "/data"
	out, err = cmd.CombinedOutput()
	if err != nil {
		db.log.Infow("unable to run pg_upgrade on new new datadir, abort upgrade", "error", err)
		return nil
	}
	db.log.Infow("pg_upgrade done", "output", string(out))

	// rm -rf /data/postgres
	err = os.RemoveAll(db.datadir)
	if err != nil {
		return fmt.Errorf("unable to remove old datadir %w", err)
	}

	err = os.Rename(newDataDirTemp, db.datadir)
	if err != nil {
		return fmt.Errorf("unable to rename upgraded datadir to destination, output:%q error %w", string(out), err)
	}

	db.log.Infow("pg_upgrade done and new data in place", "output", string(out))

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
