package redis

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/afero"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"go.uber.org/zap"
)

const (
	redisDumpFile = "dump.rdb"
)

// Redis implements the database interface
type Redis struct {
	log      *zap.SugaredLogger
	executor *utils.CmdExecutor
	datadir  string

	client *redis.Client
}

// New instantiates a new meilisearch database
func New(log *zap.SugaredLogger, datadir string, addr string, password *string) (*Redis, error) {
	if addr == "" {
		return nil, fmt.Errorf("redis addr cannot be empty")
	}

	opts := &redis.Options{
		Addr: addr,
		// FIXME check if all dbs are backed up from 0
		DB: 0,
	}
	if password != nil {
		opts.Password = *password
	}

	client := redis.NewClient(opts)

	return &Redis{
		log:      log,
		datadir:  datadir,
		executor: utils.NewExecutor(log),
		client:   client,
	}, nil
}

// Backup takes a dump of meilisearch with the meilisearch client.
func (db *Redis) Backup(ctx context.Context) error {
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	start := time.Now()
	_, err := db.client.Save(ctx).Result()
	if err != nil {
		return fmt.Errorf("could not create a dump: %w", err)
	}
	resp, err := db.client.ConfigGet(ctx, "dir").Result()
	if err != nil {
		return fmt.Errorf("could not get config: %w", err)
	}
	dumpDir := resp["dir"]
	dumpFile := path.Join(dumpDir, redisDumpFile)

	db.log.Infow("dump created successfully", "file", dumpFile, "duration", time.Since(start).String())

	// we need to do a copy here and cannot simply rename as the file system is
	// mounted by two containers. the dump is created in the database container,
	// the copy is done in the backup-restore-sidecar container. os.Rename would
	// lead to an error.

	err = utils.Copy(afero.NewOsFs(), dumpFile, path.Join(constants.BackupDir, redisDumpFile))
	if err != nil {
		return fmt.Errorf("unable to copy dumpfile to backupdir: %w", err)
	}

	err = os.Remove(dumpFile)
	if err != nil {
		return fmt.Errorf("unable to clean up dump: %w", err)
	}

	db.log.Debugw("successfully took backup of redis")
	return nil
}

// Check indicates whether a restore of the database is required or not.
func (db *Redis) Check(_ context.Context) (bool, error) {
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
func (db *Redis) Probe(ctx context.Context) error {
	_, err := db.client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	return nil
}

// Recover restores a database backup
func (db *Redis) Recover(ctx context.Context) error {
	dump := path.Join(constants.RestoreDir, redisDumpFile)

	if _, err := os.Stat(dump); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", dump)
	}

	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	start := time.Now()

	err := utils.Copy(afero.NewOsFs(), dump, path.Join(db.datadir, redisDumpFile))
	if err != nil {
		return fmt.Errorf("unable to recover %w", err)
	}

	db.log.Infow("successfully restored redis database", "duration", time.Since(start).String())

	return nil
}

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
func (db *Redis) Upgrade(_ context.Context) error {
	return nil
}
