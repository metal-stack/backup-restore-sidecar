package meilisearch

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"go.uber.org/zap"
)

// Meilisearch implements the database interface
type Meilisearch struct {
	datadir  string
	log      *zap.SugaredLogger
	executor *utils.CmdExecutor
	client   *meilisearch.Client
}

// New instantiates a new postgres database
func New(log *zap.SugaredLogger, datadir string, url string, apikey string) *Meilisearch {
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   url,
		APIKey: apikey,
	})
	return &Meilisearch{
		log:      log,
		datadir:  datadir,
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
			db.log.Infow("dump details", "details", dumpTask.Details)
			return nil
		}
		return nil
	}, nil)
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
	db.log.Error("upgrade is not yet implemented")
	return nil
}

func (db *Meilisearch) moveDumpsToBackupDir() error {
	dumpDir := path.Join(db.datadir, "dumps")
	return filepath.Walk(dumpDir, func(basepath string, d fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".dump") {
			return nil
		}

		dst := path.Join(constants.BackupDir, d.Name())
		src := path.Join(basepath, d.Name())
		db.log.Infow("move dump", "from", src, "to", dst)

		err = os.Rename(src, dst)
		if err != nil {
			return fmt.Errorf("unable move dump: %w", err)
		}

		return nil
	})
}
