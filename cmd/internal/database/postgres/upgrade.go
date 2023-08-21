package postgres

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	postgresHBAConf     = "pg_hba.conf"
	postgresConfigCmd   = "pg_config"
	postgresUpgradeCmd  = "pg_upgrade"
	postgresInitDBCmd   = "initdb"
	postgresVersionFile = "PG_VERSION"
)

var (
	requiredCommands = []string{postgresUpgradeCmd, postgresConfigCmd, postgresInitDBCmd}
)

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
//
// The function aborts the update without returning an error as long as the old data stays unmodified and only prints out the error to console.
// This behavior is intended to reduce unnecessary downtime caused by misconfigurations.
// If any preconditions are not met, no error is returned, a info log entry is created with the reason.
// Once the upgrade was made, any error condition will require to recover the database from backup.
func (db *Postgres) Upgrade() error {
	start := time.Now()

	err := db.copyPostgresBinaries()
	if err != nil {
		return err
	}

	// First check if there are data already present
	pgVersionFile := path.Join(db.datadir, postgresVersionFile)
	if _, err := os.Stat(pgVersionFile); errors.Is(err, fs.ErrNotExist) {
		db.log.Infof("%q is not present, no upgrade required", pgVersionFile)
		return nil
	}

	// Check if required commands are present
	for _, command := range requiredCommands {
		if ok := db.isCommandPresent(command); !ok {
			db.log.Infof("%q is not present, skipping upgrade", command)
			return nil
		}
	}

	// Then check the version of the existing database
	pgVersion, err := db.getDatabaseVersion(pgVersionFile)
	if err != nil {
		db.log.Infow("unable get database version", "error", err)
		return nil
	}

	// Now check the version of the actual postgres binaries
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

	oldPostgresBindir := path.Join(db.datadir, fmt.Sprintf("pg-bin-v%d", pgVersion))

	// Check if old pg_config are present and match pgVersion
	oldPostgresConfigCmd := path.Join(oldPostgresBindir, postgresConfigCmd)
	if ok := db.isCommandPresent(oldPostgresConfigCmd); !ok {
		db.log.Infof("%q is not present, skipping upgrade", oldPostgresConfigCmd)
		return nil
	}

	// We need to upgrade, therefore old binaries are required
	oldBinaryVersionMajor, err := db.getBinaryVersion(oldPostgresConfigCmd)
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

	// run the pg_upgrade command as postgres user
	pgUser, err := user.Lookup("postgres")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(pgUser.Uid)
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid)},
	}
	err = cmd.Run()
	if err != nil {
		db.log.Errorw("unable to run initdb on new new datadir, skipping upgrade", "error", err)
		return nil
	}
	db.log.Infow("new database directory initialized")

	// restore old pg_hba.conf
	pgHBAConf, err := os.ReadFile(path.Join(db.datadir, postgresHBAConf))
	if err != nil {
		return err
	}
	err = os.WriteFile(path.Join(newDataDirTemp, postgresHBAConf), pgHBAConf, 0600)
	if err != nil {
		return err
	}

	newPostgresBindir, err := db.getBindir(postgresConfigCmd)
	if err != nil {
		return fmt.Errorf("unable to detect bindir of actual postgres %w", err)
	}

	pgUpgradeArgs := []string{
		"--old-datadir", db.datadir,
		"--new-datadir", newDataDirTemp,
		"--old-bindir", oldPostgresBindir,
		"--new-bindir", newPostgresBindir,
		"--link",
	}
	db.log.Infow("running pg_upgrade with", "args", pgUpgradeArgs)
	cmd = exec.Command(postgresUpgradeCmd, pgUpgradeArgs...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid)},
	}
	cmd.Dir = pgUser.HomeDir
	err = cmd.Run()
	if err != nil {
		db.log.Errorw("unable to run pg_upgrade on new new datadir, abort upgrade", "error", err)
		return fmt.Errorf("unable to run pg_upgrade %w", err)
	}
	db.log.Infow("pg_upgrade done")

	// rm -rf /data/postgres
	err = os.RemoveAll(db.datadir)
	if err != nil {
		return fmt.Errorf("unable to remove old datadir %w", err)
	}

	err = os.Rename(newDataDirTemp, db.datadir)
	if err != nil {
		return fmt.Errorf("unable to rename upgraded datadir to destination, a full restore is required, error %w", err)
	}

	db.log.Infow("pg_upgrade done and new data in place", "took", time.Since(start))

	return nil
}

// Helpers

func (db *Postgres) getBinaryVersion(pgConfigCmd string) (int, error) {
	// pg_config  --version
	// PostgreSQL 12.16
	cmd := exec.Command(pgConfigCmd, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("unable to detect postgres binary version, skipping upgrade %w", err)
	}
	_, binaryVersionString, found := strings.Cut(string(out), "PostgreSQL ")
	if !found {
		return 0, fmt.Errorf("unable to detect postgres binary version in pg_config output, skipping upgrade, output:%q", binaryVersionString)
	}
	v, err := semver.NewVersion(strings.TrimSpace(binaryVersionString))
	if err != nil {
		return 0, fmt.Errorf("unable to parse postgres binary version in %q %w", binaryVersionString, err)
	}
	return int(v.Major()), nil
}

func (db *Postgres) getDatabaseVersion(pgVersionFile string) (int, error) {
	// cat PG_VERSION
	// 12
	pgVersionBytes, err := os.ReadFile(pgVersionFile)
	if err != nil {
		return 0, fmt.Errorf("unable to read %q %w", pgVersionFile, err)
	}
	pgVersion, err := strconv.Atoi(strings.TrimSpace(string(pgVersionBytes)))
	if err != nil {
		return 0, fmt.Errorf("unable to parse content of %q content:%q to an int %w", pgVersionFile, string(pgVersionBytes), err)
	}
	return pgVersion, nil
}

func (db *Postgres) isCommandPresent(command string) bool {
	p, err := exec.LookPath(command)
	if err != nil {
		return false
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return true
}

func (db *Postgres) getBindir(pgConfigCmd string) (string, error) {
	cmd := exec.Command(pgConfigCmd, "--bindir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (db *Postgres) copyPostgresBinaries() error {
	bindir, err := db.getBindir(postgresConfigCmd)
	if err != nil {
		return err
	}

	version, err := db.getBinaryVersion(postgresConfigCmd)
	if err != nil {
		return err
	}

	pgbindir := path.Join(db.datadir, fmt.Sprintf("pg-bin-v%d", version))

	err = os.RemoveAll(pgbindir)
	if err != nil {
		return fmt.Errorf("unable to remove old pgbindir %w", err)
	}

	db.log.Infow("copying postgres binaries for later upgrades", "from", bindir, "to", pgbindir)
	copy := exec.Command("cp", "-av", bindir, pgbindir)
	copy.Stdout = os.Stdout
	copy.Stderr = os.Stderr
	err = copy.Run()
	if err != nil {
		return fmt.Errorf("unable to copy pgbindir %w", err)
	}
	return nil
}
