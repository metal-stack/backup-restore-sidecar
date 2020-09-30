package backup

import (
	"os"
	"path"

	backuproviders "github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	cron "github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Start starts the backup component, which is periodically taking backups of the database
func Start(log *zap.SugaredLogger, backupSchedule string, db database.DatabaseProber, bp backuproviders.BackupProvider, metrics *metrics.Metrics, compressionMethod compress.Method, stop <-chan struct{}) error {
	log.Info("database is now available, starting periodic backups")

	c := cron.New()

	id, err := c.AddFunc(backupSchedule, func() {
		err := db.Backup()
		if err != nil {
			metrics.CountError("create")
			log.Errorw("database backup failed", "error", err)
			return
		}
		log.Infow("successfully backed up database")

		backupArchiveName := bp.GetNextBackupName() + ".tar.gz"

		backupFilePath := path.Join(constants.BackupDir, backupArchiveName)
		if err := os.RemoveAll(backupFilePath); err != nil {
			metrics.CountError("delete_prior")
			log.Errorw("could not delete priorly uploaded backup", "error", err)
			return
		}

		comp := compress.New(compressionMethod)
		err = comp.Compress(backupFilePath)
		if err != nil {
			metrics.CountError("compress")
			log.Errorw("unable to compress backup", "error", err)
			return
		}
		log.Info("compressed backup")

		err = bp.UploadBackup(backupFilePath)
		if err != nil {
			metrics.CountError("upload")
			log.Errorw("error uploading backup", "error", err)
			return
		}
		log.Info("uploaded backup to backup provider bucket")
		metrics.CountBackup(backupFilePath)
		err = bp.CleanupBackups()
		if err != nil {
			metrics.CountError("cleanup")
			log.Errorw("cleaning up backups failed", "error", err)
		} else {
			log.Infow("cleaned up backups")
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
	<-stop
	c.Stop()
	return nil
}
