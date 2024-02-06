package probe

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
)

var (
	probeInterval = 3 * time.Second
)

// Start starts the database prober
func Start(ctx context.Context, log *slog.Logger, db database.DatabaseProber) error {
	log.Info("start probing database")

	for {
		select {
		case <-ctx.Done():
			return errors.New("received stop signal, stop probing")
		case <-time.After(probeInterval):
			err := db.Probe(ctx)
			if err == nil {
				return nil
			}
			log.Error("database has not yet started, waiting and retrying...", "error", err)
		}
	}
}
