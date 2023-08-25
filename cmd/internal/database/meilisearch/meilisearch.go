package meilisearch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"
	"golang.org/x/sync/errgroup"

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
	latestStableDump       = "latest.dump"
)

// Meilisearch implements the database interface
type Meilisearch struct {
	dbdir               string
	dumpdir             string
	apikey              string
	latestStableDumpDst string
	log                 *zap.SugaredLogger
	executor            *utils.CmdExecutor
	client              *meilisearch.Client
}

// New instantiates a new meilisearch database
func New(log *zap.SugaredLogger, datadir string, url string, apikey string) *Meilisearch {
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   url,
		APIKey: apikey,
	})
	dbdir := path.Join(datadir, meilisearchDBDir)
	dumpdir := path.Join(datadir, meilisearchDumpDir)
	latestStableDumpDst := path.Join(dumpdir, latestStableDump)
	return &Meilisearch{
		log:                 log,
		dbdir:               dbdir,
		dumpdir:             dumpdir,
		apikey:              apikey,
		latestStableDumpDst: latestStableDumpDst,
		executor:            utils.NewExecutor(log),
		client:              client,
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
		if dumpTask.Status != meilisearch.TaskStatusSucceeded {
			return fmt.Errorf("dump still processing")
		}

		db.log.Infow("dump finished", "duration", dumpTask.Duration, "details", dumpTask.Details)
		return nil
	}, retry.Attempts(100))
	if err != nil {
		return err
	}
	err = db.moveDumpToBackupDir()
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
	dump := path.Join(constants.RestoreDir, latestStableDump)
	if _, err := os.Stat(dump); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", dump)
	}
	start := time.Now()

	if err := utils.RemoveContents(db.dbdir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	err := db.importDump(dump)
	if err != nil {
		return fmt.Errorf("unable to recover %w", err)
	}

	db.log.Infow("recovery done", "duration", time.Since(start))
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
	if _, err := os.Stat(db.latestStableDumpDst); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%q is not present, no upgrade possible, maybe no backup was running before", db.latestStableDumpDst)
	}

	db.log.Infow("start upgrade", "from", dbVersion, "to", meilisearchVersion)

	err = os.Rename(db.dbdir, db.dbdir+".old")
	if err != nil {
		return fmt.Errorf("unable to rename dbdir: %w", err)
	}

	err = db.importDump(db.latestStableDumpDst)
	if err != nil {
		return err
	}
	db.log.Infow("upgrade done and new data in place", "took", time.Since(start))
	return nil
}

func (db *Meilisearch) importDump(dump string) error {
	var (
		err  error
		cmd  *exec.Cmd
		g, _ = errgroup.WithContext(context.Background())
	)

	g.Go(func() error {
		args := []string{"--import-dump", dump, "--master-key", db.apikey}
		db.log.Infow("execute meilisearch", "args", args)

		cmd = exec.Command(meilisearchCmd, args...) // nolint:gosec
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("unable import dump %w", err)
		}
		db.log.Info("import of dump finished")
		return nil
	})

	// TODO big databases might take longer, not sure if 100 attempts are enough
	// must check how long it take max with backoff ?
	err = retry.Do(func() error {
		v, err := db.client.Version()
		if err != nil {
			return err
		}
		healthy := db.client.IsHealthy()
		if !healthy {
			return fmt.Errorf("meilisearch does not report healthiness")
		}
		db.log.Infow("meilisearch started after importing the dump, killing it", "version", v)
		return cmd.Process.Signal(syscall.SIGTERM)
	}, retry.Attempts(100))
	if err != nil {
		return err
	}
	err = g.Wait()
	if err != nil {
		// sending a TERM signal will always result in a error response.
		db.log.Infow("importing dump terminated but reported an error which can be ignored", "error", err)
	}
	return nil
}

// moveDumpToBackupDir move all dumps to the backupdir
// also create a stable last stable dump for later upgrades
func (db *Meilisearch) moveDumpToBackupDir() error {
	dumps, err := filepath.Glob(db.dumpdir + "/*.dump")
	if err != nil {
		return fmt.Errorf("unable to find dumps %w", err)
	}
	src := ""
	// sort them an take only the latest dump
	slices.Sort(dumps)
	for _, dump := range dumps {
		if strings.Contains(dump, latestStableDump) {
			continue
		}
		src = dump
	}

	db.log.Infow("create latest dump rename", "from", src, "to", db.latestStableDumpDst)
	err = os.Rename(src, db.latestStableDumpDst)
	if err != nil {
		return fmt.Errorf("unable create latest stable dump: %w", err)
	}

	backupDst := path.Join(constants.BackupDir, latestStableDump)
	db.log.Infow("move dump", "from", db.latestStableDumpDst, "to", backupDst)
	copy := exec.Command("mv", "-v", db.latestStableDumpDst, backupDst) // nolint:gosec
	copy.Stdout = os.Stdout
	copy.Stderr = os.Stderr
	err = copy.Run()
	if err != nil {
		return fmt.Errorf("unable move dump: %w", err)
	}
	return nil
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

	return v, nil
}
