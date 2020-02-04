package backup

import (
	"os"
	"path"
	"time"

	backuproviders "github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/mholt/archiver"

	"go.uber.org/zap"
)

const (
	backupArchiveName = "db.tar.gz"
)

// Start starts the backup component, which is periodically taking backups of the database
func Start(log *zap.SugaredLogger, backupInterval time.Duration, db database.DatabaseProber, bp backuproviders.BackupProvider, stop <-chan struct{}) {
	log.Info("database is now available, starting periodic backups")
	log.Infow("scheduling next backup", "in", time.Now().Add(backupInterval).String(), "interval", backupInterval.String())

	for {
		select {
		case <-stop:
			log.Info("received stop signal, shutting down")
			return
		case <-time.After(backupInterval):
			err := db.Backup()
			if err != nil {
				log.Errorw("database backup failed", "error", err)
				continue
			}
			log.Infow("successfully backed up database")

			backupFilePath := path.Join(constants.BackupDir, backupArchiveName)
			if err := os.RemoveAll(backupFilePath); err != nil {
				log.Errorw("could not delete priorly uploaded backup", "error", err)
				continue
			}
			err = compressBackup(backupFilePath)
			if err != nil {
				log.Errorw("unable to compress backup", "error", err)
				continue
			}
			log.Info("compressed backup")

			err = bp.UploadBackup(backupFilePath)
			if err != nil {
				log.Errorw("error uploading backup", "error", err)
				continue
			}
			log.Info("uploaded backup to backup provider bucket")

			err = bp.CleanupBackups()
			if err != nil {
				log.Errorw("cleaning up backups failed", "error", err)
			} else {
				log.Infow("cleaned up backups")
			}

			log.Infow("scheduling next backup", "in", time.Now().Add(backupInterval).String(), "interval", backupInterval.String())
		}
	}
}

func compressBackup(backupFilePath string) error {
	return archiver.NewTarGz().Archive([]string{constants.BackupDir}, backupFilePath)
}
