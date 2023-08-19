package etcd

import (
	"fmt"
	"os"
	"path"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
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
func (db *Etcd) Check() (bool, error) {
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
func (db *Etcd) Backup() error {
	snapshotFileName := path.Join(constants.BackupDir, "snapshot.db")
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory %w", err)
	}

	// Create a etcd snapshot.
	out, err := db.etcdctl(true, "snapshot", "save", snapshotFileName)
	if err != nil {
		return fmt.Errorf("error running backup command: %s", out)
	}

	db.log.Infow("took backup of etcd database", "output", out)

	if _, err := os.Stat(snapshotFileName); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created: %s", snapshotFileName)
	}

	out, err = db.etcdctl(false, "snapshot", "status", "--write-out", "json", snapshotFileName)
	if err != nil {
		return fmt.Errorf("backup was not created correct: %s", out)
	}

	db.log.Infow("successfully took backup of etcd database, snapshot status is", "status", out)

	return nil
}

// Recover restores a database backup
func (db *Etcd) Recover() error {
	snapshotFileName := path.Join(constants.RestoreDir, "snapshot.db")
	if _, err := os.Stat(snapshotFileName); os.IsNotExist(err) {
		return fmt.Errorf("restore file is not present: %s", snapshotFileName)
	}

	out, err := db.etcdutl("snapshot", "status", "--write-out", "json", snapshotFileName)
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

	out, err = db.etcdutl("snapshot", "restore", "--data-dir", db.datadir, snapshotFileName)
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

// Probe indicates whether the database is running
func (db *Etcd) Probe() error {
	out, err := db.etcdctl(true, "get", "foo")
	if err != nil {
		return fmt.Errorf("unable to retrieve key:%s %w", out, err)
	}
	return nil
}

// Upgrade indicates whether the database files are from a previous version of and need to be upgraded
func (db *Etcd) Upgrade() error {
	return nil
}

func (db *Etcd) etcdctl(withConnectionArgs bool, args ...string) (string, error) {
	var (
		etcdctlEnvs []string
		etcdctlArgs []string
	)

	etcdctlArgs = append(etcdctlArgs, args...)

	if withConnectionArgs {
		etcdctlArgs = append(etcdctlArgs, db.connectionArgs()...)
	}

	out, err := db.executor.ExecuteCommandWithOutput(etcdctlCommand, etcdctlEnvs, etcdctlArgs...)
	if err != nil {
		return out, fmt.Errorf("error running etcdctl command: %s", out)
	}
	return out, nil
}

func (db *Etcd) etcdutl(args ...string) (string, error) {
	var (
		etcdutlEnvs []string
		etcdutlArgs []string
	)

	etcdutlArgs = append(etcdutlArgs, args...)

	out, err := db.executor.ExecuteCommandWithOutput(etcdutlCommand, etcdutlEnvs, etcdutlArgs...)
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
