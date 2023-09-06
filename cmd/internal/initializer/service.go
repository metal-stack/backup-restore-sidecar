package initializer

import (
	"context"
	"fmt"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"go.uber.org/zap"
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
	log       *zap.SugaredLogger
	bp        providers.BackupProvider
	restoreFn func(ctx context.Context, version *providers.BackupVersion) error
}

func newBackupProviderService(log *zap.SugaredLogger, bp providers.BackupProvider, restoreFn func(ctx context.Context, version *providers.BackupVersion) error) *backupService {
	return &backupService{
		log:       log,
		bp:        bp,
		restoreFn: restoreFn,
	}
}

func (s *backupService) ListBackups(ctx context.Context, _ *v1.ListBackupsRequest) (*v1.BackupListResponse, error) {
	versions, err := s.bp.ListBackups(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.log.Infow("listbackups called")
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
	s.log.Infow("listbackups called", "response", response)

	return response, nil
}

func (s *backupService) RestoreBackup(ctx context.Context, req *v1.RestoreBackupRequest) (*v1.RestoreBackupResponse, error) {
	if req.Version == "" {
		return nil, status.Error(codes.InvalidArgument, "version to restore must be defined explicitly")
	}

	versions, err := s.bp.ListBackups(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	version, err := versions.Get(req.Version)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	err = s.restoreFn(ctx, version)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("error restoring backup: %s", err))
	}

	return &v1.RestoreBackupResponse{}, nil
}

type databaseService struct {
	log      *zap.SugaredLogger
	backupFn func() error
}

func newDatabaseService(log *zap.SugaredLogger, backupFn func() error) *databaseService {
	return &databaseService{
		log:      log,
		backupFn: backupFn,
	}
}

func (s *databaseService) CreateBackup(ctx context.Context, _ *v1.CreateBackupRequest) (*v1.CreateBackupResponse, error) {
	s.log.Infow("createbackup called")
	err := s.backupFn()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("error creating backup: %s", err))
	}
	s.log.Infow("createbackup finished")

	return &v1.CreateBackupResponse{}, nil
}
