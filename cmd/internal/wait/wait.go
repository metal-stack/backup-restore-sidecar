package wait

import (
	"context"
	"log/slog"
	"time"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/pkg/client"
)

const (
	waitInterval = 3 * time.Second
)

// Start starts a wait component that will return when the initializer server has done its job
func Start(ctx context.Context, log *slog.Logger, addr string) error {
	client, err := client.New(ctx, addr)
	if err != nil {
		return err
	}

	log.Info("waiting until initializer completes", "interval", waitInterval.String())

	for {
		select {
		case <-ctx.Done():
			log.Info("received stop signal, shutting down")
			return nil
		case <-time.After(waitInterval):
			resp, err := client.InitializerServiceClient().Status(ctx, &v1.StatusRequest{})
			if err != nil {
				log.Error("error retrieving initializer server response", "error", err)
				continue
			}

			if resp.GetStatus() == v1.StatusResponse_DONE {
				log.Info("initializer succeeded, database can be started", "message", resp.GetMessage())
				return nil
			}

			log.Info("initializer has not yet succeeded", "status", resp.GetStatus(), "message", resp.GetMessage())
		}
	}
}
