package meilisearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
)

const (
	meilisearchCmd         = "meilisearch"
	meilisearchVersionFile = "VERSION"
	meilisearchDBDir       = "data.ms"
	latestStableDump       = "latest.dump"
)

// Meilisearch implements the database interface
type Meilisearch struct {
	log                   *slog.Logger
	executor              *utils.CmdExecutor
	datadir               string
	copyBinaryAfterBackup bool

	apikey string
	client *meilisearch.Client
}

// New instantiates a new meilisearch database
func New(log *slog.Logger, datadir string, url string, apikey string) (*Meilisearch, error) {
	if url == "" {
		return nil, fmt.Errorf("meilisearch api url cannot be empty")
	}
	if apikey == "" {
		return nil, fmt.Errorf("meilisearch api key cannot be empty")
	}

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

	db.log.Info("dump creation triggered", "taskUUID", dumpResponse.TaskUID)

	dumpTask, err := db.client.WaitForTask(dumpResponse.TaskUID, meilisearch.WaitParams{
		Context:  ctx,
		Interval: time.Second,
	})
	if err != nil {
		return err
	}
	dumpDuration := dumpTask.FinishedAt.Sub(dumpTask.EnqueuedAt)
	db.log.Info("dump created successfully", "duration", dumpDuration.String())

	dumps, err := filepath.Glob(constants.BackupDir + "/*.dump")
	if err != nil {
		return fmt.Errorf("unable to find dump: %w", err)
	}
	if len(dumps) != 1 {
		return fmt.Errorf("did not find unique dump, found %d", len(dumps))
	}

	// we need to do a copy here and cannot simply rename as the file system is
	// mounted by two containers. the dump is created in the database container,
	// the copy is done in the backup-restore-sidecar container. os.Rename would
	// lead to an error.

	err = utils.Copy(afero.NewOsFs(), dumps[0], path.Join(constants.BackupDir, latestStableDump))
	if err != nil {
		return fmt.Errorf("unable to move dump to latest: %w", err)
	}

	err = os.Remove(dumps[0])
	if err != nil {
		return fmt.Errorf("unable to clean up dump: %w", err)
	}

	db.log.Debug("successfully took backup of meilisearch")

	if db.copyBinaryAfterBackup {
		// for a future upgrade, the current meilisearch binary is required
		err = db.copyMeilisearchBinary(ctx, true)
		if err != nil {
			return err
		}
	}

	return nil
}

// Check indicates whether a restore of the database is required or not.
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

	db.log.Info("successfully restored meilisearch database", "duration", time.Since(start).String())

	return nil
}

func (db *Meilisearch) importDump(ctx context.Context, dump string) error {
	var (
		err  error
		g, _ = errgroup.WithContext(ctx)

		handleFailedRecovery = func(restoreErr error) error {
			db.log.Error("trying to handle failed database recovery", "error", restoreErr)

			if err := os.RemoveAll(db.datadir); err != nil {
				db.log.Error("unable to cleanup database data directory after failed recovery attempt, high risk of starting with fresh database on container restart", "err", err)
			} else {
				db.log.Info("cleaned up database data directory after failed recovery attempt to prevent start of fresh database")
			}

			return restoreErr
		}
	)

	args := []string{"--import-dump", dump, "--master-key", db.apikey, "--dump-dir", constants.RestoreDir, "--db-path", db.datadir, "--http-addr", "localhost:1"}
	cmd := exec.CommandContext(ctx, meilisearchCmd, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	g.Go(func() error {
		db.log.Info("execute meilisearch", "args", args)

		err = cmd.Run()
		if err != nil {
			return err
		}

		db.log.Info("execution of meilisearch finished without an error")

		return nil
	})

	restoreDB, err := New(db.log, db.datadir, "http://localhost:1", db.apikey)
	if err != nil {
		return fmt.Errorf("unable to create prober")
	}

	waitForRestore := func() error {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		sem := semaphore.NewWeighted(1)

		for {
			select {
			case <-ticker.C:
				if !sem.TryAcquire(1) {
					continue
				}

				err = restoreDB.Probe(ctx)
				sem.Release(1)
				if err != nil {
					db.log.Error("meilisearch is still restoring, continue probing for readiness...", "error", err)
					continue
				}

				db.log.Info("meilisearch started after importing the dump, stopping it again for takeover from the database container")

				return nil
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during meilisearch restore")
			}
		}
	}

	if err := waitForRestore(); err != nil {
		return handleFailedRecovery(err)
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		return handleFailedRecovery(err)
	}

	err = g.Wait()
	if err != nil {
		// will probably work better in meilisearch v1.4.0: https://github.com/meilisearch/meilisearch/commit/eff8570f591fe32a6106087807e3fe8c18e8e5e4
		if strings.Contains(err.Error(), "interrupt") {
			db.log.Info("meilisearch terminated but reported an error which can be ignored", "error", err)
		} else {
			return handleFailedRecovery(err)
		}
	}

	db.log.Info("successfully restored meilisearch database")

	return nil
}
