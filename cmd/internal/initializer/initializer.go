package initializer

import (
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
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
}

func New(log *zap.SugaredLogger, addr string, db database.Database, bp providers.BackupProvider, comp *compress.Compressor) *Initializer {
	return &Initializer{
		currentStatus: &v1.StatusResponse{
			Status:  v1.StatusResponse_CHECKING,
			Message: "starting initializer server",
		},
		log:  log,
		addr: addr,
		db:   db,
		bp:   bp,
		comp: comp,
	}
}

// Start starts the initializer, which includes a server component and the initializer itself, which is potentially restoring a backup
func (i *Initializer) Start(stop <-chan struct{}) {
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

	initializerService := newService(i.currentStatus)

	v1.RegisterInitializerServiceServer(grpcServer, initializerService)

	i.log.Infow("start initializer server", "address", i.addr)

	lis, err := net.Listen("tcp", i.addr)
	if err != nil {
		i.log.Fatalf("failed to listen: %v", err)
	}

	go func() {
		<-stop
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
		i.log.Fatal(fmt.Errorf("error initializing database, shutting down:%w", err))
	}

	i.log.Info("initializer done")
	i.currentStatus.Status = v1.StatusResponse_DONE
	i.currentStatus.Message = "done"
}

func (i *Initializer) initialize() error {
	i.log.Info("start running initializer")

	i.log.Info("ensuring backup bucket")
	i.currentStatus.Message = "ensuring backup bucket"
	err := i.bp.EnsureBackupBucket()
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
