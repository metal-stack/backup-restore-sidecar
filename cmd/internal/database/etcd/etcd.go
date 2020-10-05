package etcd

import (
	"fmt"
	"os"
	"path"

	"github.com/pkg/errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"go.uber.org/zap"
)

const (
	etcdctlCommand = "etcdctl"
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
		return errors.Wrap(err, "could not clean backup directory")
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return errors.Wrap(err, "could not create backup directory")
	}

	// Create a etcd snapshot.
	out, err := db.etcdctl("snapshot", "save", snapshotFileName)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("error running backup command: %s", out))
	}
	db.log.Infow("took backup of etcd database", "output", out)

	// TODO check snapshot status after it was created
	// etcdctl snapshot status snapshot.db --write-out json
	// {"hash":3409373368,"revision":0,"totalKey":3,"totalSize":20480}
	if _, err := os.Stat(snapshotFileName); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created: %s", snapshotFileName)
	}
	out, err = db.etcdctl("snapshot", "status", "--write-out", "json", snapshotFileName)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("backup was not created correct: %s", out))
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
	out, err := db.etcdctl("snapshot", "status", "--write-out", "json", snapshotFileName)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("restored backup file was not created correct: %s", out))
	}
	db.log.Infow("successfully pulled backup of etcd database, snapshot status is", "status", out)

	if err := utils.RemoveContents(db.datadir); err != nil {
		return errors.Wrap(err, "could not clean database data directory")
	}

	if err := os.Remove(db.datadir); err != nil {
		return errors.Wrap(err, "could not remove database data directory")
	}

	out, err = db.etcdctl("snapshot", "restore", "--data-dir", db.datadir, snapshotFileName)
	if err != nil {
		return fmt.Errorf("unable to restore:%v", err)
	}

	db.log.Infow("restored etcd base backup", "output", out)

	if err := os.RemoveAll(snapshotFileName); err != nil {
		return errors.Wrap(err, "could not remove snapshot")
	}

	db.log.Infow("successfully restored etcd database")
	return nil
}

// Probe indicates whether the database is running
func (db *Etcd) Probe() error {
	out, err := db.etcdctl("get", "foo")
	if err != nil {
		return fmt.Errorf("unable to check cluster health:%s %v", out, err)
	}
	return nil
}

func (db *Etcd) etcdctl(args ...string) (string, error) {
	etcdctlEnvs := []string{"ETCDCTL_API=3"}
	etcdctlArgs := []string{}

	etcdctlArgs = append(etcdctlArgs, args...)

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

	// execute a etcdctl command
	out, err := db.executor.ExecuteCommandWithOutput(etcdctlCommand, etcdctlEnvs, etcdctlArgs...)
	if err != nil {
		return out, errors.Wrap(err, fmt.Sprintf("error running etcdctl command: %s", out))
	}
	return out, nil
}
