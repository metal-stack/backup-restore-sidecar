package initializer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/encryption"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"

	"google.golang.org/grpc"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

type Initializer struct {
	currentStatus *v1.StatusResponse
	log           *slog.Logger
	addr          string
	db            database.Database
	bp            providers.BackupProvider
	comp          *compress.Compressor
	metrics       *metrics.Metrics
	dbDataDir     string
	encrypter     *encryption.Encrypter
}

func New(log *slog.Logger, addr string, db database.Database, bp providers.BackupProvider, comp *compress.Compressor, metrics *metrics.Metrics, dbDataDir string, encrypter *encryption.Encrypter) *Initializer {
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
		encrypter: encrypter,
	}
}

// Start starts the initializer, which includes a server component and the initializer itself, which is potentially restoring a backup
func (i *Initializer) Start(ctx context.Context, backuper *backup.Backuper) error {
	opts := []grpc.ServerOption{
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(),
			// grpc_zap.StreamServerInterceptor(i.log.Desugar()), // FIXME migrate to grpc_middleware v2
			grpc_recovery.StreamServerInterceptor(),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(),
			// grpc_zap.UnaryServerInterceptor(i.log.Desugar()), // FIXME migrate to grpc_middleware v2
			grpc_recovery.UnaryServerInterceptor(),
		)),
	}

	grpcServer := grpc.NewServer(opts...)

	initializerService := newInitializerService(i.currentStatus)
	backupService := newBackupProviderService(i.bp, i.Restore)
	databaseService := newDatabaseService(func() error {
		return backuper.CreateBackup(ctx)
	})

	v1.RegisterInitializerServiceServer(grpcServer, initializerService)
	v1.RegisterBackupServiceServer(grpcServer, backupService)
	v1.RegisterDatabaseServiceServer(grpcServer, databaseService)

	i.log.Info("start initializer server", "address", i.addr)

	lis, err := net.Listen("tcp", i.addr)
	if err != nil {
		i.log.Error("failed to listen", "error", err)
		return err
	}

	go func() {
		<-ctx.Done()
		i.log.Info("received stop signal, shutting down")
		grpcServer.Stop()
	}()

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			i.log.Error("failed to serve", "error", err)
			panic(err)
		}
	}()

	err = i.initialize(ctx)
	if err != nil {
		i.log.Error("error initializing database, shutting down", "error", err)
		return err
	}

	i.currentStatus.Status = v1.StatusResponse_UPGRADING
	i.currentStatus.Message = "start upgrading database"
	err = i.db.Upgrade(ctx)
	if err != nil {
		i.log.Error("upgrade database failed", "error", err)
		return err
	}

	i.log.Info("initializer done")
	i.currentStatus.Status = v1.StatusResponse_DONE
	i.currentStatus.Message = "done"
	return nil
}

func (i *Initializer) initialize(ctx context.Context) error {
	i.log.Info("start running initializer")

	i.log.Info("ensuring database data directory")
	err := os.MkdirAll(i.dbDataDir, 0755)
	if err != nil {
		return fmt.Errorf("unable to ensure database data directory: %w", err)
	}

	i.log.Info("ensuring backup bucket")
	i.currentStatus.Message = "ensuring backup bucket"
	err = i.bp.EnsureBackupBucket(ctx)
	if err != nil {
		return fmt.Errorf("unable to ensure backup bucket: %w", err)
	}

	i.log.Info("ensuring default download directory")
	err = os.MkdirAll(constants.DownloadDir, 0755)
	if err != nil {
		return fmt.Errorf("unable to ensure default download directory: %w", err)
	}

	i.log.Info("checking database")
	i.currentStatus.Status = v1.StatusResponse_CHECKING
	i.currentStatus.Message = "checking database"

	needsBackup, err := i.db.Check(ctx)
	if err != nil {
		return fmt.Errorf("unable to check data of database: %w", err)
	}

	if !needsBackup {
		i.log.Info("database does not need to be restored")
		return nil
	}

	i.log.Info("database potentially needs to be restored, looking for backup")

	versions, err := i.bp.ListBackups(ctx)
	if err != nil {
		return fmt.Errorf("unable retrieve backup versions: %w", err)
	}

	latestBackup := versions.Latest()
	if latestBackup == nil {
		i.log.Info("there are no backups available, it's a fresh database. allow database to start")
		return nil
	}

	if i.encrypter == nil {
		if encryption.IsEncrypted(latestBackup.Name) {
			return fmt.Errorf("latest backup is encrypted, but no encryption/decryption is configured")
		}
	}

	err = i.Restore(ctx, latestBackup)
	if err != nil {
		return fmt.Errorf("unable to restore database: %w", err)
	}

	return nil
}

// Restore restores the database with the given backup version
func (i *Initializer) Restore(ctx context.Context, version *providers.BackupVersion) error {
	i.log.Info("restoring backup", "version", version.Version, "date", version.Date.String())

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

	// pipe to download and buffer downloaded data
	reader1, writer1 := io.Pipe()
	downloadErr := make(chan error, 1)
	downloadBuffer := &bytes.Buffer{}
	go func() {
		defer writer1.Close()
		defer close(downloadErr)

		err := i.bp.DownloadBackup(ctx, version, writer1)
		if err != nil {
			i.metrics.CountError("download")
			i.log.Error("error downloading backup", "error", err)
			downloadErr <- err
		} else {
			downloadErr <- nil
		}

	}()
	go func() {
		_, err := io.Copy(downloadBuffer, reader1)
		if err != nil {
			i.metrics.CountError("buffering")
			i.log.Error("error buffering downloaded data", "error", err)
		}
	}()

	err := <-downloadErr
	if err != nil {
		return fmt.Errorf("error downloading backup: %w", err)
	}

	i.currentStatus.Message = "decrypting backup"

	// pipe to decrypt and buffer decrypted data
	reader2, writer2 := io.Pipe()
	decryptErr := make(chan error, 1)
	decryptBuffer := &bytes.Buffer{}
	go func() {
		defer writer2.Close()
		defer close(decryptErr)

		if i.encrypter != nil {
			err = i.encrypter.Decrypt(downloadBuffer, writer2)
			if err != nil {
				i.metrics.CountError("decrypt")
				i.log.Error("error decrypting backup", "error", err)
				decryptErr <- err
			} else {
				decryptErr <- nil
			}
		} else {
			i.log.Info("restoring unencrypted backup with configured encryption - skipping decryption...")
			_, err := io.Copy(writer2, downloadBuffer)
			if err != nil {
				i.metrics.CountError("streaming")
				i.log.Error("error streaming downloaded data", "error", err)
				decryptErr <- err
			}
			decryptErr <- nil
		}
	}()
	go func() {
		_, err := io.Copy(decryptBuffer, reader2)
		if err != nil {
			i.metrics.CountError("streaming")
			i.log.Error("error streaming decrypted data", "error", err)
		}
	}()

	err = <-decryptErr
	if err != nil {
		return fmt.Errorf("error decrypting backup: %w", err)
	}

	i.currentStatus.Message = "uncompressing backup"
	err = i.comp.Decompress(ctx, decryptBuffer)
	if err != nil {
		return fmt.Errorf("unable to uncompress backup: %w", err)
	}

	files, err := os.ReadDir(constants.RestoreDir)
	for _, file := range files {
		i.log.Info("restored file", "file", file.Name())
	}

	i.currentStatus.Message = "restoring backup"
	err = i.db.Recover(ctx)
	if err != nil {
		return fmt.Errorf("restoring database was not successful: %w", err)
	}

	return nil
}
