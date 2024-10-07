package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"

	backuproviders "github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/encryption"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	cron "github.com/robfig/cron/v3"
	"golang.org/x/sync/semaphore"
)

type BackuperConfig struct {
	Log            *slog.Logger
	BackupSchedule string
	DatabaseProber database.DatabaseProber
	BackupProvider backuproviders.BackupProvider
	Metrics        *metrics.Metrics
	Compressor     *compress.Compressor
	Encrypter      *encryption.Encrypter
}

type Backuper struct {
	log            *slog.Logger
	backupSchedule string
	db             database.DatabaseProber
	bp             backuproviders.BackupProvider
	metrics        *metrics.Metrics
	comp           *compress.Compressor
	sem            *semaphore.Weighted
	encrypter      *encryption.Encrypter
}

func New(config *BackuperConfig) *Backuper {
	return &Backuper{
		log:            config.Log,
		backupSchedule: config.BackupSchedule,
		db:             config.DatabaseProber,
		bp:             config.BackupProvider,
		metrics:        config.Metrics,
		comp:           config.Compressor,
		// sem guards backups to be taken concurrently
		sem:       semaphore.NewWeighted(1),
		encrypter: config.Encrypter,
	}
}

// Start starts the backup component, which is periodically taking backups of the database
func (b *Backuper) Start(ctx context.Context) error {
	b.log.Info("database is now available, starting periodic backups")

	c := cron.New()

	id, err := c.AddFunc(b.backupSchedule, func() {
		err := b.CreateBackup(ctx)
		if err != nil {
			b.log.Error("error creating backup", "error", err)
		}

		for _, e := range c.Entries() {
			b.log.Info("scheduling next backup", "at", e.Next.String())
		}
	})
	if err != nil {
		return err
	}

	c.Start()
	b.log.Info("scheduling next backup", "at", c.Entry(id).Next.String())
	<-ctx.Done()
	c.Stop()
	return nil
}

func (b *Backuper) CreateBackup(ctx context.Context) error {
	if !b.sem.TryAcquire(1) {
		return constants.ErrBackupAlreadyInProgress
	}
	defer b.sem.Release(1)

	err := b.db.Backup(ctx)
	if err != nil {
		b.metrics.CountError("create")
		return fmt.Errorf("database backup failed: %w", err)
	}

	b.log.Info("successfully backed up database")

	backupArchiveName := b.bp.GetNextBackupName(ctx)

	backupFilePath := path.Join(constants.BackupDir, backupArchiveName)
	if err := os.RemoveAll(backupFilePath + b.comp.Extension()); err != nil {
		b.metrics.CountError("delete_prior")
		return fmt.Errorf("could not delete priorly uploaded backup: %w", err)
	}

	filename, err := b.comp.Compress(backupFilePath)
	if err != nil {
		b.metrics.CountError("compress")
		return fmt.Errorf("unable to compress backup: %w", err)
	}

	b.log.Info("compressed backup")

	if b.encrypter != nil {
		filename, err = b.encrypter.Encrypt(filename)
		if err != nil {
			b.metrics.CountError("encrypt")
			return fmt.Errorf("error encrypting backup: %w", err)
		}
		b.log.Info("encrypted backup")
	}

	err = b.bp.UploadBackup(ctx, filename)
	if err != nil {
		b.metrics.CountError("upload")
		return fmt.Errorf("error uploading backup: %w", err)
	}

	b.log.Info("uploaded backup to backup provider bucket")

	b.metrics.CountBackup(filename)

	err = b.bp.CleanupBackups(ctx)
	if err != nil {
		b.metrics.CountError("cleanup")
		b.log.Error("cleaning up backups failed", "error", err)
	} else {
		b.log.Info("cleaned up backups")
	}

	return nil
}
