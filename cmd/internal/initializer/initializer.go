package initializer

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"go.uber.org/zap"

	"google.golang.org/grpc"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

type Initializer struct {
	currentStatus *v1.StatusResponse
	log           *zap.SugaredLogger
	addr          string
	db            database.Database
	bp            providers.BackupProvider
	comp          *compress.Compressor
	metrics       *metrics.Metrics
	dbDataDir     string
}

func New(log *zap.SugaredLogger, addr string, db database.Database, bp providers.BackupProvider, comp *compress.Compressor, metrics *metrics.Metrics, dbDataDir string) *Initializer {
	return &Initializer{
		currentStatus: &v1.StatusResponse{
			Status:  v1.StatusResponse_CHECKING,
			Message: "starting initializer server",
		},
		log:       log,
		addr:      addr,
		db:        db,
		bp:        bp,
		comp:      comp,
		dbDataDir: dbDataDir,
		metrics:   metrics,
	}
}

// Start starts the initializer, which includes a server component and the initializer itself, which is potentially restoring a backup
func (i *Initializer) Start(ctx context.Context) {
	opts := []grpc.ServerOption{
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(),
			grpc_zap.StreamServerInterceptor(i.log.Desugar()),
			grpc_recovery.StreamServerInterceptor(),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(),
			grpc_zap.UnaryServerInterceptor(i.log.Desugar()),
			grpc_recovery.UnaryServerInterceptor(),
		)),
	}

	grpcServer := grpc.NewServer(opts...)

	initializerService := newInitializerService(i.currentStatus)
	backupService := newBackupProviderService(i.bp, i.Restore)
	databaseService := newDatabaseService(func() error {
		return backup.CreateBackup(i.log, i.db, i.bp, i.metrics, i.comp)
	})

	v1.RegisterInitializerServiceServer(grpcServer, initializerService)
	v1.RegisterBackupServiceServer(grpcServer, backupService)
	v1.RegisterDatabaseServiceServer(grpcServer, databaseService)

	i.log.Infow("start initializer server", "address", i.addr)

	lis, err := net.Listen("tcp", i.addr)
	if err != nil {
		i.log.Fatalf("failed to listen: %v", err)
	}

	go func() {
		<-ctx.Done()
		i.log.Info("received stop signal, shutting down")
		grpcServer.Stop()
	}()

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			i.log.Fatalf("failed to serve: %v", err)
		}
	}()

	err = i.initialize()
	if err != nil {
		i.log.Fatalw("error initializing database, shutting down", "error", err)
	}

	i.currentStatus.Status = v1.StatusResponse_UPGRADING
	i.currentStatus.Message = "start upgrading database"
	err = i.db.Upgrade()
	if err != nil {
		i.log.Fatalw("upgrade database failed", "error", err)
	}

	i.log.Info("initializer done")
	i.currentStatus.Status = v1.StatusResponse_DONE
	i.currentStatus.Message = "done"
}

func (i *Initializer) initialize() error {
	i.log.Info("start running initializer")

	i.log.Info("ensuring database data directory")
	err := os.MkdirAll(i.dbDataDir, 0755)
	if err != nil {
		return fmt.Errorf("unable to ensure database data directory: %w", err)
	}

	i.log.Info("ensuring backup bucket")
	i.currentStatus.Message = "ensuring backup bucket"
	err = i.bp.EnsureBackupBucket()
	if err != nil {
		return fmt.Errorf("unable to ensure backup bucket: %w", err)
	}

	i.log.Info("checking database")
	i.currentStatus.Status = v1.StatusResponse_CHECKING
	i.currentStatus.Message = "checking database"

	needsBackup, err := i.db.Check()
	if err != nil {
		return fmt.Errorf("unable to check data of database: %w", err)
	}

	if !needsBackup {
		i.log.Info("database does not need to be restored")
		return nil
	}

	i.log.Info("database potentially needs to be restored, looking for backup")

	versions, err := i.bp.ListBackups()
	if err != nil {
		return fmt.Errorf("unable retrieve backup versions: %w", err)
	}

	latestBackup := versions.Latest()
	if latestBackup == nil {
		i.log.Info("there are no backups available, it's a fresh database. allow database to start")
		return nil
	}

	err = i.Restore(latestBackup)
	if err != nil {
		return fmt.Errorf("unable to restore database: %w", err)
	}

	return nil
}

// Restore restores the database with the given backup version
func (i *Initializer) Restore(version *providers.BackupVersion) error {
	i.log.Infow("restoring backup", "version", version.Version, "date", version.Date.String())

	i.currentStatus.Status = v1.StatusResponse_RESTORING
	i.currentStatus.Message = "prepare restore"

	if err := os.RemoveAll(constants.RestoreDir); err != nil {
		return fmt.Errorf("could not clean restore directory: %w", err)
	}

	if err := os.MkdirAll(constants.RestoreDir, 0777); err != nil {
		return fmt.Errorf("could not create restore directory: %w", err)
	}

	i.currentStatus.Message = "downloading backup"

	downloadFileName := version.Name
	if strings.Contains(downloadFileName, "/") {
		downloadFileName = filepath.Base(downloadFileName)
	}
	backupFilePath := path.Join(constants.DownloadDir, downloadFileName)
	if err := os.RemoveAll(backupFilePath); err != nil {
		return fmt.Errorf("could not delete priorly downloaded file: %w", err)
	}

	err := i.bp.DownloadBackup(version)
	if err != nil {
		return fmt.Errorf("unable to download backup: %w", err)
	}

	i.currentStatus.Message = "uncompressing backup"
	err = i.comp.Decompress(backupFilePath)
	if err != nil {
		return fmt.Errorf("unable to uncompress backup: %w", err)
	}

	i.currentStatus.Message = "restoring backup"
	err = i.db.Recover()
	if err != nil {
		return fmt.Errorf("restoring database was not successful: %w", err)
	}

	return nil
}
