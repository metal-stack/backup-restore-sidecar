package initializer

import (
	"context"
	"fmt"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/common"
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

func (s *initializerService) Status(context.Context, *v1.StatusRequest) (*v1.StatusResponse, error) {
	return s.currentStatus, nil
}

type backupService struct {
	bp        providers.BackupProvider
	restoreFn func(ctx context.Context, version *providers.BackupVersion, downloadOnly bool) error
}

func newBackupProviderService(bp providers.BackupProvider, restoreFn func(ctx context.Context, version *providers.BackupVersion, downloadOnly bool) error) *backupService {
	return &backupService{
		bp:        bp,
		restoreFn: restoreFn,
	}
}

func (s *backupService) ListBackups(ctx context.Context, _ *v1.ListBackupsRequest) (*v1.BackupListResponse, error) {
	versions, err := s.bp.ListBackups(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// List internally sorts the backups
	backups := versions.List()
	common.Sort(backups)

	response := &v1.BackupListResponse{}
	for _, b := range backups {
		b := b
		response.Backups = append(response.GetBackups(), &v1.Backup{
			Name:      b.Name,
			Version:   b.Version,
			Timestamp: timestamppb.New(b.Date),
		})
	}

	return response, nil
}

func (s *backupService) RestoreBackup(ctx context.Context, req *v1.RestoreBackupRequest) (*v1.RestoreBackupResponse, error) {
	if req.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "version to restore must be defined explicitly")
	}

	versions, err := s.bp.ListBackups(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	version, err := versions.Get(req.GetVersion())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	//TODO -> check false value here
	err = s.restoreFn(ctx, version, false)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("error restoring backup: %s", err))
	}

	return &v1.RestoreBackupResponse{}, nil
}

func (s *backupService) GetBackupByVersion(ctx context.Context, req *v1.GetBackupByVersionRequest) (*v1.GetBackupByVersionResponse, error) {
	if req.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "version to get must be defined explicitly")
	}

	versions, err := s.bp.ListBackups(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	version, err := versions.Get(req.GetVersion())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &v1.GetBackupByVersionResponse{Backup: &v1.Backup{Name: version.Name, Version: version.Version, Timestamp: timestamppb.New(version.Date)}}, nil

}

type databaseService struct {
	backupFn func() error
}

func newDatabaseService(backupFn func() error) *databaseService {
	return &databaseService{
		backupFn: backupFn,
	}
}

func (s *databaseService) CreateBackup(ctx context.Context, _ *v1.CreateBackupRequest) (*v1.CreateBackupResponse, error) {
	err := s.backupFn()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("error creating backup: %s", err))
	}

	return &v1.CreateBackupResponse{}, nil
}
