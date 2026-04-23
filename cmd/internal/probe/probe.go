package probe

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
)

const (
	probeInterval    = 3 * time.Second
	probeCallTimeout = 10 * time.Second
)

// Start starts the database prober
func Start(ctx context.Context, log *slog.Logger, db database.DatabaseProber) error {
	log.Info("start probing database")

	probeTicker := time.NewTicker(probeInterval)
	defer probeTicker.Stop()

	var lastErrMsg string
	var lastLogTime time.Time

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("timeout reached while probing database")
			}
			return errors.New("received stop signal, shutting down")
		case <-probeTicker.C:
			probeCtx, cancelProbe := context.WithTimeout(ctx, probeCallTimeout)
			err := db.Probe(probeCtx)
			cancelProbe()

			if err == nil {
				return nil
			}

			errMsg := err.Error()
			if errMsg != lastErrMsg || time.Since(lastLogTime) > 1*time.Minute {
				log.Warn("database has not yet started, waiting and retrying...", "error", err)
				lastErrMsg = errMsg
				lastLogTime = time.Now()
			}
		}
	}
}
