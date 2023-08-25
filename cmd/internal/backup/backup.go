package backup

import (
	"context"
	"fmt"
	"os"
	"path"

	backuproviders "github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	cron "github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Start starts the backup component, which is periodically taking backups of the database
func Start(ctx context.Context, log *zap.SugaredLogger, backupSchedule string, db database.DatabaseProber, bp backuproviders.BackupProvider, metrics *metrics.Metrics, comp *compress.Compressor) error {
	log.Info("database is now available, starting periodic backups")

	c := cron.New()

	id, err := c.AddFunc(backupSchedule, func() {
		err := CreateBackup(log, db, bp, metrics, comp)
		if err != nil {
			log.Errorw("error creating backup", "error", err)
		}

		for _, e := range c.Entries() {
			log.Infow("scheduling next backup", "at", e.Next.String())
		}
	})
	if err != nil {
		return err
	}

	c.Start()
	log.Infow("scheduling next backup", "at", c.Entry(id).Next.String())
	<-ctx.Done()
	c.Stop()
	return nil
}

func CreateBackup(log *zap.SugaredLogger, db database.DatabaseProber, bp backuproviders.BackupProvider, metrics *metrics.Metrics, comp *compress.Compressor) error {
	err := db.Backup()
	if err != nil {
		metrics.CountError("create")
		return fmt.Errorf("database backup failed: %w", err)
	}

	log.Infow("successfully backed up database")

	backupArchiveName := bp.GetNextBackupName()

	backupFilePath := path.Join(constants.BackupDir, backupArchiveName)
	if err := os.RemoveAll(backupFilePath + comp.Extension()); err != nil {
		metrics.CountError("delete_prior")
		return fmt.Errorf("could not delete priorly uploaded backup: %w", err)
	}

	filename, err := comp.Compress(backupFilePath)
	if err != nil {
		metrics.CountError("compress")
		return fmt.Errorf("unable to compress backup: %w", err)
	}
	log.Info("compressed backup")

	err = bp.UploadBackup(filename)
	if err != nil {
		metrics.CountError("upload")
		return fmt.Errorf("error uploading backup: %w", err)
	}
	log.Info("uploaded backup to backup provider bucket")

	metrics.CountBackup(filename)

	err = bp.CleanupBackups()
	if err != nil {
		metrics.CountError("cleanup")
		log.Errorw("cleaning up backups failed", "error", err)
	} else {
		log.Infow("cleaned up backups")
	}

	return nil
}
