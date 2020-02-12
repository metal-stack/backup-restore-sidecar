package backup

import (
	"os"
	"path"

	backuproviders "github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/mholt/archiver"
	cron "github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Start starts the backup component, which is periodically taking backups of the database
func Start(log *zap.SugaredLogger, backupSchedule string, db database.DatabaseProber, bp backuproviders.BackupProvider, stop <-chan struct{}) error {
	log.Info("database is now available, starting periodic backups")

	c := cron.New()

	id, err := c.AddFunc(backupSchedule, func() {
		err := db.Backup()
		if err != nil {
			log.Errorw("database backup failed", "error", err)
			return
		}
		log.Infow("successfully backed up database")

		backupArchiveName := bp.GetNextBackupName() + ".tar.gz"

		backupFilePath := path.Join(constants.BackupDir, backupArchiveName)
		if err := os.RemoveAll(backupFilePath); err != nil {
			log.Errorw("could not delete priorly uploaded backup", "error", err)
			return
		}
		err = compressBackup(backupFilePath)
		if err != nil {
			log.Errorw("unable to compress backup", "error", err)
			return
		}
		log.Info("compressed backup")

		err = bp.UploadBackup(backupFilePath)
		if err != nil {
			log.Errorw("error uploading backup", "error", err)
			return
		}
		log.Info("uploaded backup to backup provider bucket")

		err = bp.CleanupBackups()
		if err != nil {
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

func compressBackup(backupFilePath string) error {
	return archiver.NewTarGz().Archive([]string{constants.BackupDir}, backupFilePath)
}
