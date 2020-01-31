package wait

import (
	"context"
	"time"

	v1 "github.com/metal-pod/backup-restore-sidecar/api/v1"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/initializer"
	"go.uber.org/zap"
)

const (
	waitInterval = 3 * time.Second
)

func Start(log *zap.SugaredLogger, addr string, stop <-chan struct{}) error {
	client, err := initializer.NewInitializerClientWithRetry(addr, log)
	if err != nil {
		return err
	}

	log.Infow("waiting until initializer completes", "interval", waitInterval.String())

	for {
		select {
		case <-stop:
			log.Info("received stop signal, shutting down")
			return nil
		case <-time.After(waitInterval):
			log.Infow("waiting until initializer completes", "interval", waitInterval.String())

			resp, err := client.Status(context.Background(), &v1.Empty{})
			if err != nil {
				log.Errorw("error retrieving initializer server response", "error", err)
				continue
			}

			if resp.Status == v1.StatusResponse_DONE {
				log.Infow("initializer succeeded, database can be started", "message", resp.Message)
				return nil
			}

			log.Infow("initializer has not yet succeeded", "status", resp.Status, "message", resp.Message)
		}
	}
}
