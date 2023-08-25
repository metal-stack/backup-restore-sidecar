package initializer

import (
	"context"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type initializerService struct {
	currentStatus *v1.StatusResponse
}

func newInitializerService(currentStatus *v1.StatusResponse) *initializerService {
	return &initializerService{
		currentStatus: currentStatus,
	}
}

func (s *initializerService) Status(context.Context, *v1.Empty) (*v1.StatusResponse, error) {
	return s.currentStatus, nil
}

type backupService struct {
	bp providers.BackupProvider
}

func newBackupProviderService(bp providers.BackupProvider) *backupService {
	return &backupService{
		bp: bp,
	}
}

func (s *backupService) ListBackups(context.Context, *v1.Empty) (*v1.BackupListResponse, error) {
	versions, err := s.bp.ListBackups()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	backups := versions.List()
	versions.Sort(backups, false)

	response := &v1.BackupListResponse{}
	for _, b := range backups {
		b := b
		response.Backups = append(response.Backups, &v1.Backup{
			Name:      b.Name,
			Version:   b.Version,
			Timestamp: timestamppb.New(b.Date),
		})
	}

	return response, nil
}
