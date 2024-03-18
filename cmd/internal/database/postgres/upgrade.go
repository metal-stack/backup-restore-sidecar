package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
)

const (
	postgresHBAConf         = "pg_hba.conf"
	postgresqlConf          = "postgresql.conf"
	postgresConfigCmd       = "pg_config"
	postgresUpgradeCmd      = "pg_upgrade"
	postgresInitDBCmd       = "initdb"
	postgresVersionFile     = "PG_VERSION"
	postgresBinBackupPrefix = "pg-bin-v"
)

var (
	requiredCommands = []string{postgresUpgradeCmd, postgresConfigCmd, postgresInitDBCmd}
)

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
func (db *Postgres) Upgrade(ctx context.Context) error {
	start := time.Now()

	// First check if there are data already present
	pgVersionFile := path.Join(db.datadir, postgresVersionFile)
	if _, err := os.Stat(pgVersionFile); errors.Is(err, fs.ErrNotExist) {
		db.log.Info("pgversion file is not present, no upgrade required", "file", pgVersionFile)
		return nil
	}

	// If this is a database directory, save actual postgres binaries for a later major upgrade
	err := db.copyPostgresBinaries(ctx, true)
	if err != nil {
		return err
	}

	// Check if required commands are present
	for _, command := range requiredCommands {
		if ok := utils.IsCommandPresent(command); !ok {
			db.log.Error("command is not present, skipping upgrade", "command", command)
			return nil
		}
	}

	// Then check the version of the existing database
	pgVersion, err := db.getDatabaseVersion(pgVersionFile)
	if err != nil {
		db.log.Error("unable get database version, skipping upgrade", "error", err)
		return nil
	}

	// Now check the version of the actual postgres binaries
	binaryVersionMajor, err := db.getBinaryVersion(ctx, postgresConfigCmd)
	if err != nil {
		db.log.Error("unable to get binary version, skipping upgrade", "error", err)
		return nil
	}

	if pgVersion == binaryVersionMajor {
		db.log.Info("no version difference, no upgrade required", "database-version", pgVersion, "binary-version", binaryVersionMajor)
		return nil
	}
	if pgVersion > binaryVersionMajor {
		db.log.Error("database is newer than postgres binary, aborting", "database-version", pgVersion, "binary-version", binaryVersionMajor)
		return fmt.Errorf("database is newer than postgres binary")
	}

	oldPostgresBinDir := path.Join(db.datadir, fmt.Sprintf("%s%d", postgresBinBackupPrefix, pgVersion))

	// Check if old pg_config are present and match pgVersion
	oldPostgresConfigCmd := path.Join(oldPostgresBinDir, postgresConfigCmd)
	if ok := utils.IsCommandPresent(oldPostgresConfigCmd); !ok {
		db.log.Info("command is not present, please make sure that at least one backup was taken with the old postgres version or restart the backup-restore-sidecar container with the old postgres version before running an upgrade, skipping upgrade", "command", oldPostgresConfigCmd)
		return nil
	}

	// We need to upgrade, therefore old binaries are required
	oldBinaryVersionMajor, err := db.getBinaryVersion(ctx, oldPostgresConfigCmd)
	if err != nil {
		db.log.Error("unable to get old binary version, skipping upgrade", "error", err)
		return nil
	}

	if oldBinaryVersionMajor != pgVersion {
		db.log.Error("database version and old binary version do not match, skipping upgrade", "old database", pgVersion, "old binary", oldBinaryVersionMajor)
		return nil
	}

	// OK we need to upgrade the database in place, maybe taking a backup before is recommended
	db.log.Info("start upgrading from", "old database", pgVersion, "old binary", oldBinaryVersionMajor, "new binary", binaryVersionMajor)

	// run the pg_upgrade command as postgres user
	pgUser, err := user.Lookup("postgres")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(pgUser.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(pgUser.Gid)
	if err != nil {
		return err
	}

	// remove /data/postgres-new if present
	newDataDirTemp := path.Join("/data", "postgres-new") // TODO: /data should not be hardcoded
	err = os.RemoveAll(newDataDirTemp)
	if err != nil {
		db.log.Error("unable to remove new datadir, skipping upgrade", "error", err)
		return nil
	}

	err = os.Chown("/data", uid, gid)
	if err != nil {
		return err
	}

	// initdb -D /data/postgres-new
	cmd := exec.Command(postgresInitDBCmd, "-D", newDataDirTemp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid)},
	}
	err = cmd.Run()
	if err != nil {
		db.log.Error("unable to run initdb on new new datadir, skipping upgrade", "error", err)
		return nil
	}

	db.log.Info("new database directory initialized")

	// restore old pg_hba.conf and postgresql.conf
	for _, config := range []string{postgresHBAConf, postgresqlConf} {
		db.log.Info("restore old configuration into new datadir", "config", config)

		cfg, err := os.ReadFile(path.Join(db.datadir, config))
		if err != nil {
			return err
		}

		err = os.WriteFile(path.Join(newDataDirTemp, config), cfg, 0600)
		if err != nil {
			return err
		}
	}

	err = db.restoreOldPostgresBinaries(db.datadir, newDataDirTemp)
	if err != nil {
		return err
	}

	newPostgresBinDir, err := db.getBinDir(ctx, postgresConfigCmd)
	if err != nil {
		return fmt.Errorf("unable to detect bin dir of actual postgres %w", err)
	}

	pgUpgradeArgs := []string{
		"--old-datadir", db.datadir,
		"--new-datadir", newDataDirTemp,
		"--old-bindir", oldPostgresBinDir,
		"--new-bindir", newPostgresBinDir,
		"--link",
	}

	runsTimescaleDB, err := db.runningTimescaleDB(ctx, postgresConfigCmd)
	if err != nil {
		return err
	}

	if runsTimescaleDB {
		// see https://github.com/timescale/timescaledb/issues/1844 and https://github.com/timescale/timescaledb/issues/4503#issuecomment-1860883843
		db.log.Info("running timescaledb, applying custom options for upgrade command")

		// timescaledb libraries in this container are only compatible with the current postgres version
		// do not load them anymore with the old postgresql server
		pgUpgradeArgs = append(pgUpgradeArgs,
			"--old-options", "-c shared_preload_libraries=''",
			"--new-options", "-c timescaledb.restoring=on -c shared_preload_libraries=timescaledb",
		)
	}

	cmd = exec.CommandContext(ctx, postgresUpgradeCmd, pgUpgradeArgs...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid)},
	}
	cmd.Dir = pgUser.HomeDir

	db.log.Info("running pg_upgrade with", "args", pgUpgradeArgs)
	err = cmd.Run()
	if err != nil {
		db.log.Error("unable to run pg_upgrade on new new datadir, abort upgrade", "error", err)
		return fmt.Errorf("unable to run pg_upgrade %w", err)
	}

	db.log.Info("pg_upgrade done")

	// rm -rf /data/postgres
	err = os.RemoveAll(db.datadir)
	if err != nil {
		return fmt.Errorf("unable to remove old data dir: %w", err)
	}

	err = os.Rename(newDataDirTemp, db.datadir)
	if err != nil {
		return fmt.Errorf("unable to rename upgraded datadir to destination, a full restore is required: %w", err)
	}

	db.log.Info("pg_upgrade done and new data in place", "took", time.Since(start).String())

	return nil
}

// Helpers

func (db *Postgres) getBinaryVersion(ctx context.Context, pgConfigCmd string) (int, error) {
	// pg_config  --version
	// PostgreSQL 12.16
	cmd := exec.CommandContext(ctx, pgConfigCmd, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("unable to detect postgres binary version: %w", err)
	}

	_, binaryVersionString, found := strings.Cut(string(out), "PostgreSQL ")
	if !found {
		return 0, fmt.Errorf("unable to detect postgres binary version in pg_config output %q", binaryVersionString)
	}

	v, err := semver.NewVersion(strings.TrimSpace(binaryVersionString))
	if err != nil {
		return 0, fmt.Errorf("unable to parse postgres binary version in %q: %w", binaryVersionString, err)
	}

	return int(v.Major()), nil
}

func (db *Postgres) getDatabaseVersion(pgVersionFile string) (int, error) {
	// cat PG_VERSION
	// 12
	pgVersionBytes, err := os.ReadFile(pgVersionFile)
	if err != nil {
		return 0, fmt.Errorf("unable to read %q: %w", pgVersionFile, err)
	}

	pgVersion, err := strconv.Atoi(strings.TrimSpace(string(pgVersionBytes)))
	if err != nil {
		return 0, fmt.Errorf("unable to convert content of %q (content: %q) to integer: %w", pgVersionFile, string(pgVersionBytes), err)
	}

	return pgVersion, nil
}

func (db *Postgres) getBinDir(ctx context.Context, pgConfigCmd string) (string, error) {
	cmd := exec.CommandContext(ctx, pgConfigCmd, "--bindir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

func (db *Postgres) runningTimescaleDB(ctx context.Context, pgConfigCmd string) (bool, error) {
	libDir, err := db.getLibDir(ctx, pgConfigCmd)
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(path.Join(libDir, "timescaledb.so")); err == nil {
		return true, nil
	}

	return false, nil
}

func (db *Postgres) getLibDir(ctx context.Context, pgConfigCmd string) (string, error) {
	cmd := exec.CommandContext(ctx, pgConfigCmd, "--pkglibdir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("unable to figure out lib dir: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// copyPostgresBinaries is needed to save old postgres binaries for a later major upgrade
func (db *Postgres) copyPostgresBinaries(ctx context.Context, override bool) error {
	binDir, err := db.getBinDir(ctx, postgresConfigCmd)
	if err != nil {
		return err
	}

	version, err := db.getBinaryVersion(ctx, postgresConfigCmd)
	if err != nil {
		return err
	}

	pgBinDir := path.Join(db.datadir, fmt.Sprintf("%s%d", postgresBinBackupPrefix, version))

	if !override {
		if _, err := os.Stat(path.Join(pgBinDir, postgresConfigCmd)); err == nil {
			db.log.Info("postgres binaries for later upgrade already in place, not copying")
			return nil
		}
	}

	err = os.RemoveAll(pgBinDir)
	if err != nil {
		return fmt.Errorf("unable to remove old pg bin dir: %w", err)
	}

	db.log.Info("copying postgres binaries for later upgrades", "from", binDir, "to", pgBinDir)
	copy := exec.CommandContext(ctx, "cp", "-av", binDir, pgBinDir)
	copy.Stdout = os.Stdout
	copy.Stderr = os.Stderr
	err = copy.Run()
	if err != nil {
		return fmt.Errorf("unable to copy pg bin dir: %w", err)
	}

	return nil
}

func (db *Postgres) restoreOldPostgresBinaries(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasPrefix(d.Name(), postgresBinBackupPrefix) {
			return nil
		}

		db.log.Info("copying postgres binaries from old datadir to new datadir", "from", path, "to", dst)

		copy := exec.Command("cp", "-av", path, dst)
		copy.Stdout = os.Stdout
		copy.Stderr = os.Stderr
		err = copy.Run()
		if err != nil {
			return fmt.Errorf("unable to copy pg bin dir: %w", err)
		}

		return nil
	})
}
