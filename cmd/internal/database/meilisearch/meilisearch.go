package meilisearch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"go.uber.org/zap"
)

const (
	meilisearchCmd         = "meilisearch"
	meilisearchVersionFile = "VERSION"
	meilisearchDBDir       = "data.ms"
	meilisearchDumpDir     = "dumps"
	dumpExtension          = ".dump"
	latestStableDump       = "forupgrade.latestdump"
)

// Meilisearch implements the database interface
type Meilisearch struct {
	dbdir    string
	dumpdir  string
	log      *zap.SugaredLogger
	executor *utils.CmdExecutor
	client   *meilisearch.Client
}

// New instantiates a new meilisearch database
func New(log *zap.SugaredLogger, datadir string, url string, apikey string) *Meilisearch {
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   url,
		APIKey: apikey,
	})
	return &Meilisearch{
		log:      log,
		dbdir:    path.Join(datadir, meilisearchDBDir),
		dumpdir:  path.Join(datadir, meilisearchDumpDir),
		executor: utils.NewExecutor(log),
		client:   client,
	}
}

// Backup implements database.Database.
func (db *Meilisearch) Backup() error {
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	dumpResponse, err := db.client.CreateDump()
	if err != nil {
		return fmt.Errorf("could not create a dump: %w", err)
	}

	db.log.Infow("dump creation triggered", "response", dumpResponse)

	err = retry.Do(func() error {
		dumpTask, err := db.client.GetTask(dumpResponse.TaskUID)
		if err != nil {
			return err
		}
		switch dumpTask.Status {
		case meilisearch.TaskStatusFailed:
			return fmt.Errorf("dump failed with:%s", dumpTask.Error.Message)
		case meilisearch.TaskStatusProcessing:
			return fmt.Errorf("dump still processing")
		case meilisearch.TaskStatusEnqueued:
			return fmt.Errorf("dump enqueued")
		case meilisearch.TaskStatusUnknown:
			return fmt.Errorf("dump status unknown")
		case meilisearch.TaskStatusSucceeded:
			db.log.Infow("dump finished", "duration", dumpTask.Duration, "details", dumpTask.Details)
			return nil
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = db.moveDumpsToBackupDir()
	if err != nil {
		return err
	}

	db.log.Debugw("successfully took backup of meilisearch")

	return nil
}

// Check implements database.Database.
func (db *Meilisearch) Check() (bool, error) {
	empty, err := utils.IsEmpty(db.dbdir)
	if err != nil {
		return false, err
	}
	if empty {
		db.log.Info("data directory is empty")
		return true, err
	}

	return false, nil
}

// Probe implements database.Database.
func (db *Meilisearch) Probe() error {
	_, err := db.client.Version()
	if err != nil {
		return fmt.Errorf("connection error:%w", err)
	}
	return nil
}

// Recover implements database.Database.
func (db *Meilisearch) Recover() error {
	db.log.Error("recover is not yet implemented")
	return nil
}

// Upgrade implements database.Database.
func (db *Meilisearch) Upgrade() error {
	start := time.Now()

	versionFile := path.Join(db.dbdir, meilisearchVersionFile)
	if _, err := os.Stat(versionFile); errors.Is(err, fs.ErrNotExist) {
		db.log.Infof("%q is not present, no upgrade required", versionFile)
		return nil
	}

	dbVersion, err := db.getDatabaseVersion(versionFile)
	if err != nil {
		return err
	}
	meilisearchVersion, err := db.getBinaryVersion()
	if err != nil {
		return err
	}
	if (dbVersion.Major() == meilisearchVersion.Major()) && (dbVersion.Minor() == meilisearchVersion.Minor()) {
		db.log.Infow("no version difference, no upgrade required", "database-version", dbVersion, "binary-version", meilisearchVersion)
		return nil
	}
	if dbVersion.GreaterThan(meilisearchVersion) {
		db.log.Errorw("database is newer than meilisearch binary, aborting", "database-version", dbVersion, "binary-version", meilisearchVersion)
		return fmt.Errorf("database is newer than meilisearch binary")
	}

	db.log.Infow("start upgrade", "from", dbVersion, "to", meilisearchVersion)

	// meilisearch --import-dump /dumps/20200813-042312213.dump
	cmd := exec.Command(meilisearchCmd, "--import-dump", " --ignore-dump-if-db-exists", path.Join(db.dumpdir, latestStableDump)) // nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		db.log.Errorw("unable import latest dump, skipping upgrade", "error", err)
		return nil
	}
	db.log.Infow("upgrade done and new data in place", "took", time.Since(start))
	return nil
}

// moveDumpsToBackupDir move all dumps to the backupdir
// also create a stable last stable dump for later upgrades
func (db *Meilisearch) moveDumpsToBackupDir() error {
	return filepath.Walk(db.dumpdir, func(basepath string, d fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(d.Name(), dumpExtension) {
			return nil
		}

		dst := path.Join(constants.BackupDir, d.Name())
		src := basepath
		db.log.Infow("move dump", "from", src, "to", dst)

		latestStableDst := path.Join(db.dumpdir, latestStableDump)
		db.log.Infow("create latest dump", "from", src, "to", latestStableDst)
		err = utils.Copy(src, latestStableDst)
		if err != nil {
			return fmt.Errorf("unable create latest stable dump: %w", err)
		}

		copy := exec.Command("mv", "-v", src, dst)
		copy.Stdout = os.Stdout
		copy.Stderr = os.Stderr
		err = copy.Run()
		if err != nil {
			return fmt.Errorf("unable move dump: %w", err)
		}

		return nil
	})
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
		return nil, fmt.Errorf("unable to parse postgres binary version in %q: %w", string(versionBytes), err)
	}
	// TODO check major
	return v, nil
}

func (db *Meilisearch) getBinaryVersion() (*semver.Version, error) {
	// meilisearch  --version
	// 1.2.0
	cmd := exec.Command(meilisearchCmd, "--version")
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

	// TODO check major
	return v, nil
}
