package etcd

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"go.uber.org/zap"
)

const (
	etcdctlCommand = "etcdctl"
	etcdutlCommand = "etcdutl"
)

// Etcd Backup
type Etcd struct {
	caCert    string
	cert      string
	endpoints string
	log       *zap.SugaredLogger
	key       string
	name      string

	datadir  string
	executor *utils.CmdExecutor
}

// New instantiates a new etcd database
func New(log *zap.SugaredLogger, datadir, caCert, cert, key, endpoints, name string) *Etcd {
	return &Etcd{
		log:       log,
		datadir:   datadir,
		name:      name,
		executor:  utils.NewExecutor(log),
		caCert:    caCert,
		cert:      cert,
		endpoints: endpoints,
		key:       key,
	}
}

// Check checks whether a backup needs to be restored or not, returns true if it needs a backup
func (db *Etcd) Check(_ context.Context) (bool, error) {
	empty, err := utils.IsEmpty(db.datadir)
	if err != nil {
		return false, err
	}
	if empty {
		db.log.Info("data directory is empty")
		return true, err
	}

	return false, nil
}

// Backup takes a full Backup of etcd with etcdctl.
func (db *Etcd) Backup(ctx context.Context) error {
	snapshotFileName := path.Join(constants.BackupDir, "snapshot.db")
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory %w", err)
	}

	// Create a etcd snapshot.
	out, err := db.etcdctl(ctx, true, "snapshot", "save", snapshotFileName)
	if err != nil {
		return fmt.Errorf("error running backup command: %s", out)
	}

	db.log.Infow("took backup of etcd database", "output", out)

	if _, err := os.Stat(snapshotFileName); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created: %s", snapshotFileName)
	}

	out, err = db.etcdctl(ctx, false, "snapshot", "status", "--write-out", "json", snapshotFileName)
	if err != nil {
		return fmt.Errorf("backup was not created correct: %s", out)
	}

	db.log.Infow("successfully took backup of etcd database, snapshot status is", "status", out)

	return nil
}

// Recover restores a database backup
func (db *Etcd) Recover(ctx context.Context) error {
	snapshotFileName := path.Join(constants.RestoreDir, "snapshot.db")
	if _, err := os.Stat(snapshotFileName); os.IsNotExist(err) {
		return fmt.Errorf("restore file is not present: %s", snapshotFileName)
	}

	out, err := db.etcdutl(ctx, "snapshot", "status", "--write-out", "json", snapshotFileName)
	if err != nil {
		return fmt.Errorf("restored backup file was not created correct: %s", out)
	}

	db.log.Infow("successfully pulled backup of etcd database, snapshot status is", "status", out)

	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory %w", err)
	}

	if err := os.Remove(db.datadir); err != nil {
		return fmt.Errorf("could not remove database data directory %w", err)
	}

	out, err = db.etcdutl(ctx, "snapshot", "restore", "--data-dir", db.datadir, snapshotFileName)
	if err != nil {
		return fmt.Errorf("unable to restore:%w", err)
	}

	db.log.Infow("restored etcd base backup", "output", out)

	if err := os.RemoveAll(snapshotFileName); err != nil {
		return fmt.Errorf("could not remove snapshot %w", err)
	}

	db.log.Infow("successfully restored etcd database")

	return nil
}

// Probe figures out if the database is running and available for taking backups.
func (db *Etcd) Probe(ctx context.Context) error {
	out, err := db.etcdctl(ctx, true, "get", "foo")
	if err != nil {
		return fmt.Errorf("unable to retrieve key:%s %w", out, err)
	}
	return nil
}

// Upgrade performs an upgrade of the database in case a newer version of the database is detected.
func (db *Etcd) Upgrade(ctx context.Context) error {
	return nil
}

func (db *Etcd) etcdctl(ctx context.Context, withConnectionArgs bool, args ...string) (string, error) {
	var (
		etcdctlEnvs []string
		etcdctlArgs []string
	)

	etcdctlArgs = append(etcdctlArgs, args...)

	if withConnectionArgs {
		etcdctlArgs = append(etcdctlArgs, db.connectionArgs()...)
	}

	out, err := db.executor.ExecuteCommandWithOutput(ctx, etcdctlCommand, etcdctlEnvs, etcdctlArgs...)
	if err != nil {
		return out, fmt.Errorf("error running etcdctl command: %s", out)
	}
	return out, nil
}

func (db *Etcd) etcdutl(ctx context.Context, args ...string) (string, error) {
	var (
		etcdutlEnvs []string
		etcdutlArgs []string
	)

	etcdutlArgs = append(etcdutlArgs, args...)

	out, err := db.executor.ExecuteCommandWithOutput(ctx, etcdutlCommand, etcdutlEnvs, etcdutlArgs...)
	if err != nil {
		return out, fmt.Errorf("error running etcdutl command: %s", out)
	}
	return out, nil
}

func (db *Etcd) connectionArgs() []string {
	etcdctlArgs := []string{}
	if db.endpoints != "" {
		etcdctlArgs = append(etcdctlArgs, "--endpoints", db.endpoints)
	}
	if db.caCert != "" {
		etcdctlArgs = append(etcdctlArgs, "--cacert", db.caCert)
	}
	if db.cert != "" {
		etcdctlArgs = append(etcdctlArgs, "--cert", db.cert)
	}
	if db.key != "" {
		etcdctlArgs = append(etcdctlArgs, "--key", db.key)
	}

	etcdctlArgs = append(etcdctlArgs, "--dial-timeout=10s", "--command-timeout=30s")
	return etcdctlArgs
}
