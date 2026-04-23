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
	grpcTimeout  = 10 * time.Second
)

// Start starts a wait component that will return when the initializer server has done its job
func Start(ctx context.Context, log *slog.Logger, addr string) error {
	client, err := client.New(ctx, addr)
	if err != nil {
		return err
	}

	log.Info("waiting until initializer completes", "interval", waitInterval.String())

	waitTicker := time.NewTicker(waitInterval)
	defer waitTicker.Stop()

	var lastStatus v1.StatusResponse_InitializerStatus
	var lastMessage string
	var lastLogTime time.Time

	for {
		select {
		case <-ctx.Done():
			log.Info("received stop signal, shutting down")
			return nil
		case <-waitTicker.C:
			grpcCtx, cancel := context.WithTimeout(ctx, grpcTimeout)
			resp, err := client.InitializerServiceClient().Status(grpcCtx, &v1.StatusRequest{})
			cancel()

			if err != nil {
				log.Error("error retrieving initializer server response", "error", err)
				continue
			}

			if resp.GetStatus() == v1.StatusResponse_DONE {
				log.Info("initializer succeeded, database can be started", "message", resp.GetMessage())
				return nil
			}
			if resp.GetStatus() != lastStatus || resp.GetMessage() != lastMessage || time.Since(lastLogTime) > 1*time.Minute {
				log.Info("initializer has not yet succeeded", "status", resp.GetStatus(), "message", resp.GetMessage())
				lastStatus = resp.GetStatus()
				lastMessage = resp.GetMessage()
				lastLogTime = time.Now()
			}
		}
	}
}
