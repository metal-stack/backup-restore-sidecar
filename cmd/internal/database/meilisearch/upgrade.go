package meilisearch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/avast/retry-go/v4"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"golang.org/x/sync/errgroup"
)

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
func (db *Meilisearch) Upgrade(ctx context.Context) error {
	start := time.Now()

	versionFile := path.Join(db.datadir, meilisearchVersionFile)
	if _, err := os.Stat(versionFile); errors.Is(err, fs.ErrNotExist) {
		db.log.Info("version file is not present, no upgrade required", "file", versionFile)
		return nil
	}

	dbVersion, err := db.getDatabaseVersion(versionFile)
	if err != nil {
		return err
	}

	meilisearchVersion, err := db.getBinaryVersion(ctx)
	if err != nil {
		db.log.Error("unable to get binary version, skipping upgrade", "error", err)
		return nil
	}

	if dbVersion.String() == meilisearchVersion.String() {
		db.log.Info("no version difference, no upgrade required", "database-version", dbVersion, "binary-version", meilisearchVersion)
		return nil
	}
	if dbVersion.GreaterThan(meilisearchVersion) {
		db.log.Error("database is newer than meilisearch binary, aborting", "database-version", dbVersion, "binary-version", meilisearchVersion)
		return fmt.Errorf("database is newer than meilisearch binary")
	}

	if ok := utils.IsCommandPresent(db.previousBinaryPath()); !ok {
		db.log.Info("command is not present, please make sure that at least one backup was taken with the old meilisearch version, skipping upgrade", "command", db.previousBinaryPath())
		return nil
	}

	db.log.Info("start upgrade", "from", dbVersion, "to", meilisearchVersion)

	err = db.dumpWithOldBinary(ctx)
	if err != nil {
		return fmt.Errorf("unable to create dump with old meilisearch binary: %w", err)
	}

	oldVersionDataDir := strings.TrimRight(db.datadir, "/") + ".upgrade"

	err = os.Rename(db.datadir, oldVersionDataDir)
	if err != nil {
		return fmt.Errorf("cannot move old version data dir out of the way, which could have happened due to a failed recovery attempt, consider manual cleanup: %w", err)
	}

	dump := path.Join(constants.BackupDir, latestStableDump)

	err = db.importDump(ctx, dump)
	if err != nil {
		return fmt.Errorf("unable to import dump with new meilisearch binary: %w", err)
	}

	err = os.RemoveAll(oldVersionDataDir)
	if err != nil {
		db.log.Error("unable cleanup old version data dir, consider manual cleanup", "error", err)
	}

	db.log.Info("meilisearch upgrade done and new data in place", "took", time.Since(start).String())

	return nil
}

// copyMeilisearchBinary is needed to save the old meilisearch binary for a later upgrade
func (db *Meilisearch) copyMeilisearchBinary(ctx context.Context, override bool) error {
	binPath, err := exec.LookPath(meilisearchCmd)
	if err != nil {
		return err
	}

	if !override {
		if _, err := os.Stat(path.Join(binPath, meilisearchCmd)); err == nil {
			db.log.Info("meilisearch binary for later upgrade already in place, not copying")
			return nil
		}
	}

	err = os.RemoveAll(db.previousBinaryPath())
	if err != nil {
		return fmt.Errorf("unable to remove old meilisearch bin dir: %w", err)
	}

	err = os.MkdirAll(path.Dir(db.previousBinaryPath()), 0777)
	if err != nil {
		return fmt.Errorf("unable to create versioned bin dir in data directory")
	}

	db.log.Info("copying meilisearch binary for later upgrades", "from", binPath, "to", db.previousBinaryPath())

	copy := exec.CommandContext(ctx, "cp", "-av", binPath, db.previousBinaryPath())
	copy.Stdout = os.Stdout
	copy.Stderr = os.Stderr
	err = copy.Run()
	if err != nil {
		return fmt.Errorf("unable to copy meilisearch binary: %w", err)
	}

	return nil
}

// make sure this is still inside the mounted data directory otherwise the upgrade won't work
func (db *Meilisearch) previousBinaryPath() string {
	return path.Join(db.datadir, "..", "previous-binary", "meilisearch")
}

func (db *Meilisearch) getDatabaseVersion(versionFile string) (*semver.Version, error) {
	// cat VERSION
	// 1.2.0
	versionBytes, err := os.ReadFile(versionFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read %q: %w", versionFile, err)
	}

	v, err := semver.NewVersion(strings.TrimSpace(string(versionBytes)))
	if err != nil {
		return nil, fmt.Errorf("unable to parse meilisearch binary version in %q: %w", string(versionBytes), err)
	}

	return v, nil
}

func (db *Meilisearch) getBinaryVersion(ctx context.Context) (*semver.Version, error) {
	// meilisearch --version
	// 1.2.0
	cmd := exec.CommandContext(ctx, meilisearchCmd, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("unable to detect meilisearch binary version: %w", err)
	}

	_, binaryVersionString, found := strings.Cut(string(out), "meilisearch ")
	if !found {
		return nil, fmt.Errorf("unable to detect meilisearch binary version in %q", binaryVersionString)
	}

	v, err := semver.NewVersion(strings.TrimSpace(binaryVersionString))
	if err != nil {
		return nil, fmt.Errorf("unable to parse meilisearch binary version in %q: %w", binaryVersionString, err)
	}

	return v, nil
}

func (db *Meilisearch) dumpWithOldBinary(ctx context.Context) error {
	var (
		err  error
		g, _ = errgroup.WithContext(ctx)
	)

	args := []string{"--master-key", db.apikey, "--dump-dir", constants.BackupDir, "--db-path", db.datadir, "--http-addr", "localhost:1"}
	cmd := exec.CommandContext(ctx, db.previousBinaryPath(), args...) // nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	g.Go(func() error {
		db.log.Info("execute previous meilisearch version", "args", args)

		err = cmd.Run()
		if err != nil {
			return err
		}

		return nil
	})

	restoreDB, err := New(db.log, db.datadir, "http://localhost:1", db.apikey)
	if err != nil {
		return fmt.Errorf("unable to create prober")
	}

	restoreDB.copyBinaryAfterBackup = false

	err = retry.Do(func() error {
		err = restoreDB.Probe(ctx)
		if err != nil {
			db.log.Error("meilisearch is still starting, continue probing for readiness...", "error", err)

			return err
		}

		db.log.Info("previous meilisearch started and is now ready for backup")
		return nil
	}, retry.Context(ctx))
	if err != nil {
		return err
	}

	err = restoreDB.Backup(ctx)
	if err != nil {
		return fmt.Errorf("unable to create dump from previous meilisearch version")
	}

	db.log.Info("taken dump from previous meilisearch version, stopping it again")

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		return err
	}

	err = g.Wait()
	if err != nil {
		// will probably work better in meilisearch v1.4.0: https://github.com/meilisearch/meilisearch/commit/eff8570f591fe32a6106087807e3fe8c18e8e5e4
		if strings.Contains(err.Error(), "interrupt") {
			db.log.Info("meilisearch terminated but reported an error which can be ignored", "error", err)
		} else {
			return err
		}
	}

	db.log.Info("successfully took dump with previous meilisearch version")

	return nil
}
