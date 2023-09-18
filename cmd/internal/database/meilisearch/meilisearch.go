package meilisearch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"go.uber.org/zap"
)

const (
	meilisearchCmd         = "meilisearch"
	meilisearchVersionFile = "VERSION"
	meilisearchDBDir       = "data.ms"
	latestStableDump       = "latest.dump"
)

// Meilisearch implements the database interface
type Meilisearch struct {
	log                   *zap.SugaredLogger
	executor              *utils.CmdExecutor
	datadir               string
	copyBinaryAfterBackup bool

	apikey string
	client *meilisearch.Client
}

// New instantiates a new meilisearch database
func New(log *zap.SugaredLogger, datadir string, url string, apikey string) (*Meilisearch, error) {
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   url,
		APIKey: apikey,
	})

	return &Meilisearch{
		log:                   log,
		datadir:               datadir,
		apikey:                apikey,
		executor:              utils.NewExecutor(log),
		client:                client,
		copyBinaryAfterBackup: true,
	}, nil
}

// Backup takes a dump of meilisearch with the meilisearch client.
func (db *Meilisearch) Backup(ctx context.Context) error {
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

	db.log.Infow("dump creation triggered", "taskUUID", dumpResponse.TaskUID)

	dumpTask, err := db.client.WaitForTask(dumpResponse.TaskUID, meilisearch.WaitParams{Context: ctx})
	if err != nil {
		return err
	}
	db.log.Infow("dump created successfully", "duration", dumpTask.Duration)

	dumps, err := filepath.Glob(constants.BackupDir + "/*.dump")
	if err != nil {
		return fmt.Errorf("unable to find dump: %w", err)
	}
	if len(dumps) == 0 {
		return fmt.Errorf("did not find unique dump, found %d", len(dumps))
	}

	err = utils.Copy(afero.NewOsFs(), dumps[0], path.Join(constants.BackupDir, latestStableDump))
	if err != nil {
		return fmt.Errorf("unable to move dump to latest: %w", err)
	}

	err = os.Remove(dumps[0])
	if err != nil {
		return fmt.Errorf("unable to clean up dump: %w", err)
	}

	db.log.Debugw("successfully took backup of meilisearch")

	if db.copyBinaryAfterBackup {
		// for a future upgrade, the current meilisearch binary is required
		err = db.copyMeilisearchBinary(ctx, true)
		if err != nil {
			return err
		}
	}

	return nil
}

// Check checks whether a backup needs to be restored or not, returns true if it needs a backup
func (db *Meilisearch) Check(_ context.Context) (bool, error) {
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

// Probe figures out if the database is running and available for taking backups.
func (db *Meilisearch) Probe(_ context.Context) error {
	_, err := db.client.Version()
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	healthy := db.client.IsHealthy()
	if !healthy {
		return fmt.Errorf("meilisearch does not report healthiness")
	}

	return nil
}

// Recover restores a database backup
func (db *Meilisearch) Recover(ctx context.Context) error {
	dump := path.Join(constants.RestoreDir, latestStableDump)

	if _, err := os.Stat(dump); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", dump)
	}

	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	start := time.Now()

	err := db.importDump(ctx, dump)
	if err != nil {
		return fmt.Errorf("unable to recover %w", err)
	}

	db.log.Infow("recovery done", "duration", time.Since(start))

	return nil
}

func (db *Meilisearch) importDump(ctx context.Context, dump string) error {
	var (
		err  error
		g, _ = errgroup.WithContext(ctx)

		handleFailedRecovery = func(restoreErr error) error {
			db.log.Errorw("trying to handle failed database recovery", "error", restoreErr)

			if err := os.RemoveAll(db.datadir); err != nil {
				db.log.Errorw("unable to cleanup database data directory after failed recovery attempt, high risk of starting with fresh database on container restart", "err", err)
			} else {
				db.log.Info("cleaned up database data directory after failed recovery attempt to prevent start of fresh database")
			}

			return restoreErr
		}
	)

	args := []string{"--import-dump", dump, "--master-key", db.apikey, "--dump-dir", constants.RestoreDir, "--db-path", db.datadir, "--http-addr", "localhost:1"}
	cmd := exec.CommandContext(ctx, meilisearchCmd, args...) // nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	g.Go(func() error {
		db.log.Infow("execute meilisearch", "args", args)

		err = cmd.Run()
		if err != nil {
			return err
		}

		db.log.Info("import of dump finished")

		return nil
	})

	// TODO big databases might take longer, not sure if 100 attempts are enough
	// must check how long it take max with backoff ?
	err = retry.Do(func() error {
		restoreDB, err := New(db.log, db.datadir, "http://localhost:1", db.apikey)
		if err != nil {
			return fmt.Errorf("unable to create prober")
		}

		err = restoreDB.Probe(ctx)
		if err != nil {
			return err
		}

		db.log.Infow("meilisearch started after importing the dump, stopping it again")

		return cmd.Process.Signal(syscall.SIGINT)
	}, retry.Attempts(100), retry.Context(ctx))
	if err != nil {
		return handleFailedRecovery(err)
	}

	err = g.Wait()
	if err != nil {
		// will probably work better in meilisearch v1.4.0: https://github.com/meilisearch/meilisearch/commit/eff8570f591fe32a6106087807e3fe8c18e8e5e4
		if strings.Contains(err.Error(), "interrupt") {
			db.log.Infow("meilisearch terminated but reported an error which can be ignored", "error", err)
		} else {
			return handleFailedRecovery(err)
		}
	}

	db.log.Info("successfully restored meilisearch database")

	return nil
}
