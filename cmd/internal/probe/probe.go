package probe

import (
	"time"

	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/database"
	"go.uber.org/zap"
)

var (
	probeInterval = 3 * time.Second
)

// Start starts the database prober
func Start(log *zap.SugaredLogger, db database.DatabaseProber, bp providers.BackupProvider, stop <-chan struct{}) {
	log.Info("start probing database")

	for {
		select {
		case <-stop:
			log.Info("received stop signal, shutting down")
			return
		case <-time.After(probeInterval):
			err := db.Probe()
			if err == nil {
				return
			}
			log.Errorw("database can not yet be started, waiting and retrying...", "error", err)
		}
	}
}
