package initializer

import (
	"context"
	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
)

type service struct {
	currentStatus *v1.StatusResponse
}

func newService(currentStatus *v1.StatusResponse) service {
	return service{
		currentStatus: currentStatus,
	}
}

func (i service) Status(context.Context, *v1.Empty) (*v1.StatusResponse, error) {
	return i.currentStatus, nil
}
