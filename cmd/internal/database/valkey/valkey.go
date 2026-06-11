package valkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/afero"
)

const (
	valkeyDumpFile = "dump.rdb"
)

type Valkey struct {
	log     *slog.Logger
	datadir string

	client *redis.Client

	masterReplicaMode bool
	password          string

	podName string
}

func (db *Valkey) Check(context.Context) (bool, error) {
	empty, err := utils.IsEmpty(db.datadir)
	if err != nil {
		return false, err
	}
	if empty {
		db.log.Info("data directory is empty")
	}
	return empty, nil
}

func (db *Valkey) Recover(context.Context) error {
	dump := path.Join(constants.RestoreDir, valkeyDumpFile)
	if _, err := os.Stat(dump); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", dump)
	}

	if !db.masterReplicaMode {
		return db.performRestore(dump)
	}

	// In master-replica mode, only pod-0 (the Valkey master) restores from backup.
	// Replicas skip restore and sync data from the master via Valkey replication.
	// The ordinal is deterministic from the StatefulSet pod name — no distributed
	// coordination is needed because each pod already knows its own role.
	ordinal := extractOrdinalFromPodName(db.podName)
	if ordinal == -1 {
		return fmt.Errorf("failed to extract pod ordinal from POD_NAME: %s", db.podName)
	}

	if ordinal != 0 {
		db.log.Info("not pod-0, skipping restore - will sync from master via replication",
			"ordinal", ordinal)
		return nil
	}

	db.log.Info("pod-0 (will be Valkey master), performing restore from backup")
	return db.performRestore(dump)
}

func (db *Valkey) performRestore(dump string) error {
	if err := utils.RemoveContents(db.datadir); err != nil {
		return fmt.Errorf("could not clean database data directory: %w", err)
	}

	err := utils.Copy(afero.NewOsFs(), dump, path.Join(db.datadir, valkeyDumpFile))
	if err != nil {
		return fmt.Errorf("unable to recover: %w", err)
	}
	db.log.Info("successfully restored valkey database")
	return nil
}

func (db *Valkey) Upgrade(context.Context) error {
	return nil
}

func (db *Valkey) Backup(ctx context.Context) error {
	// Scheduled backups are already filtered through ShouldPerformBackup, but a
	// backup can also be triggered manually through the gRPC API, so the role is
	// verified here again to guarantee that a backup is never taken from a replica.
	isMaster, err := db.isMaster(ctx)
	if err != nil {
		return err
	}
	if !isMaster {
		if db.masterReplicaMode {
			return errors.New("this instance is a replica, backups can only be taken on the master")
		}
		// Without master-replica mode every instance of an externally managed
		// replication setup runs the backup cron and the replicas simply skip,
		// same as the redis implementation.
		db.log.Info("this database is not master, not taking a backup")
		return nil
	}

	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	start := time.Now()

	_, err = db.client.Save(ctx).Result()
	if err != nil {
		return fmt.Errorf("could not create a dump: %w", err)
	}

	dumpFile := path.Join(db.datadir, valkeyDumpFile)
	db.log.Info("dump created successfully", "file", dumpFile, "duration", time.Since(start).String())

	err = utils.Copy(afero.NewOsFs(), dumpFile, path.Join(constants.BackupDir, valkeyDumpFile))
	if err != nil {
		return fmt.Errorf("unable to copy dumpfile to backupdir: %w", err)
	}

	// the dump deliberately stays in the data directory: together with valkey's
	// automatic snapshotting it is the local recovery point for pod restarts with
	// an intact volume, so a restore from backup is only needed when the volume
	// is actually lost.

	db.log.Debug("successfully took backup of valkey")
	return nil
}

func New(
	log *slog.Logger,
	datadir string,
	addr string,
	password *string,
	masterReplicaMode bool) (*Valkey, error) {
	var pw string
	if password != nil {
		pw = *password
	}

	v := &Valkey{
		log:               log,
		datadir:           datadir,
		password:          pw,
		masterReplicaMode: masterReplicaMode,
		podName:           os.Getenv("POD_NAME"),
	}

	log.Info("creating valkey instance", "masterReplicaMode", masterReplicaMode, "addr", addr)

	v.client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pw,
	})

	return v, nil
}

func (db *Valkey) Probe(ctx context.Context) error {
	_, err := db.client.Ping(ctx).Result()
	return err
}

func (db *Valkey) ShouldPerformBackup(ctx context.Context) bool {
	if !db.masterReplicaMode {
		return true
	}

	// In master-replica mode, only the Valkey master (pod-0) should take backups.
	// We check the database role directly via INFO replication because it is the
	// authoritative source of truth at runtime.
	isMaster, err := db.isMaster(ctx)
	if err != nil {
		db.log.Warn("failed to check Valkey master status, skipping backup", "error", err)
		return false
	}

	if !isMaster {
		db.log.Debug("not the Valkey master, skipping backup")
		return false
	}

	db.log.Debug("this is the Valkey master, performing backup")
	return true
}

func (db *Valkey) isMaster(ctx context.Context) (bool, error) {
	info, err := db.client.Info(ctx, "replication").Result()
	if err != nil {
		return false, fmt.Errorf("unable to get database info: %w", err)
	}

	return strings.Contains(info, "role:master"), nil
}

// Close gracefully shuts down the Valkey database connection.
func (db *Valkey) Close() error {
	if db.client != nil {
		if err := db.client.Close(); err != nil {
			return fmt.Errorf("failed to close valkey client: %w", err)
		}
	}

	return nil
}

// extractOrdinalFromPodName extracts the ordinal number from a StatefulSet pod name.
// Expected format: <statefulset-name>-<ordinal> (e.g., valkey-master-replica-0)
// Returns the ordinal number or -1 if extraction fails.
func extractOrdinalFromPodName(podName string) int {
	if podName == "" {
		return -1
	}

	lastDash := strings.LastIndex(podName, "-")
	if lastDash == -1 || lastDash == len(podName)-1 {
		return -1
	}

	ordinalStr := podName[lastDash+1:]
	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil {
		return -1
	}

	return ordinal
}
