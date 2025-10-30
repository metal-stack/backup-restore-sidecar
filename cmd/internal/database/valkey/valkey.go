package valkey

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/leaderelection"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/afero"
)

const (
	valkeyDumpFile = "dump.rdb"
)

type Valkey struct {
	log      *slog.Logger
	executor *utils.CmdExecutor
	datadir  string

	client *redis.Client

	clusterMode     bool
	clusterSize     int
	statefulSetName string
	password        string

	leaderElection       *leaderelection.LeaderElection
	leaderElectionCancel context.CancelFunc
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

func (db *Valkey) Recover(ctx context.Context) error {
	dump := path.Join(constants.RestoreDir, valkeyDumpFile)
	if _, err := os.Stat(dump); os.IsNotExist(err) {
		return fmt.Errorf("restore file not present: %s", dump)
	}

	if db.clusterMode {
		if db.leaderElection == nil {
			return fmt.Errorf("leader election not initialized")
		}

		db.log.Info("cluster mode: waiting for leader election before restore")

		/* Wait for leader election to complete before restore.
		Timeout calculation (90 seconds):
		LeaseDuration: 60s (maximum time for a failed leader's lease to expire)
		RetryPeriod: 5s (time for a new leader to acquire the lease after expiration)
		Buffer: 25s (accounts for pod startup time, Kubernetes API latency, and clock skew)
		If this timeout is reached, the pod is not the leader and will rely on Valkey replication
		to sync data from the master instead of restoring from backup.
		*/
		waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		startTime := time.Now()
		for {
			select {
			case <-waitCtx.Done():
				// Timeout reached
				elapsed := time.Since(startTime)
				db.log.Info("leader election timeout - not the leader, will sync from master via replication",
					"elapsed", elapsed.String())
				return nil
			case <-ticker.C:
				if db.leaderElection.IsLeader() {
					elapsed := time.Since(startTime)

					// Leader election considers database role: only restore if this pod will be the Valkey master
					// In master-replica mode, pod-0 is always the master (determined by init.sh)
					podName := os.Getenv("POD_NAME")
					ordinal := extractOrdinalFromPodName(podName)

					if ordinal == -1 {
						return fmt.Errorf("failed to extract pod ordinal from POD_NAME: %s", podName)
					}

					if ordinal != 0 {
						db.log.Info("elected as backup leader but not pod-0 (won't be Valkey master), skipping restore",
							"ordinal", ordinal,
							"electionTime", elapsed.String())
						return nil
					}

					db.log.Info("elected as backup leader AND pod-0 (will be Valkey master), performing restore from backup",
						"ordinal", ordinal,
						"electionTime", elapsed.String())
					return db.performRestore(dump)
				}
				// Still waiting for election log progress every 10 seconds
				elapsed := time.Since(startTime)
				if int(elapsed.Seconds())%10 == 0 && elapsed.Seconds() > 0 {
					db.log.Debug("waiting for leader election",
						"elapsed", elapsed.String(),
						"timeout", "90s")
				}
			}
		}
	}
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
	if !db.clusterMode {
		isMaster, err := db.isMaster(ctx)
		if err != nil {
			return err
		}
		if !isMaster {
			db.log.Info("this database is not master, not taking a backup")
			return nil
		}
	}

	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	start := time.Now()

	_, err := db.client.Save(ctx).Result()
	if err != nil {
		return fmt.Errorf("could not create a dump: %w", err)
	}

	dumpFile := path.Join(db.datadir, valkeyDumpFile)
	db.log.Info("dump created successfully", "file", dumpFile, "duration", time.Since(start).String())

	err = utils.Copy(afero.NewOsFs(), dumpFile, path.Join(constants.BackupDir, valkeyDumpFile))
	if err != nil {
		return fmt.Errorf("unable to copy dumpfile to backupdir: %w", err)
	}

	err = os.Remove(dumpFile)
	if err != nil {
		return fmt.Errorf("unable to clean up dump: %w", err)
	}

	db.log.Debug("successfully took backup of valkey")
	return nil
}

func New(
	log *slog.Logger,
	datadir string,
	password *string,
	statefulSetName string,
	clusterMode bool,
	clusterSize int) (*Valkey, error) {
	v := &Valkey{
		log:             log,
		datadir:         datadir,
		password:        getPassword(password),
		executor:        utils.NewExecutor(log),
		clusterMode:     clusterMode,
		clusterSize:     clusterSize,
		statefulSetName: statefulSetName,
	}

	log.Info("Creating Valkey instance", "clusterMode", clusterMode, "clusterSize", clusterSize)

	v.client = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: getPassword(password),
	})

	if !clusterMode {
		return v, nil
	}
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")

	if podName == "" {
		return nil, fmt.Errorf("cluster mode requires POD_NAME environment variable to be set")
	}
	if podNamespace == "" {
		return nil, fmt.Errorf("cluster mode requires POD_NAMESPACE environment variable to be set")
	}

	log.Info("cluster mode environment validated", "podName", podName, "namespace", podNamespace)
	leaderElection, err := leaderelection.New(leaderelection.Config{
		Log:      log,
		LockName: fmt.Sprintf("valkey-backup-restore-%s", v.statefulSetName),
		OnStartedLeading: func(ctx context.Context) {
			log.Info("became leader for valkey master-replica backup/restore coordination")
		},
		OnStoppedLeading: func() {
			log.Info("stopped being leader for valkey master-replica backup/restore coordination")
		},
		OnNewLeader: func(identity string) {
			log.Info("new leader elected for valkey master-replica backup/restore coordination",
				"leader", identity)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create leader election: %w", err)
	}
	v.leaderElection = leaderElection

	log.Info("cluster mode enabled with leader election coordination")

	leCtx, leCancel := context.WithCancel(context.Background())
	v.leaderElectionCancel = leCancel

	go func() {
		if err := leaderElection.Start(leCtx); err != nil {
			log.Error("leader election failed", "error", err)
		}
	}()

	return v, nil
}

func (db *Valkey) Probe(ctx context.Context) error {
	_, err := db.client.Ping(ctx).Result()
	if err != nil {
		return err
	}

	if db.clusterMode && db.leaderElection != nil {
		isLeader := db.leaderElection.IsLeader()
		db.log.Debug("cluster mode probe", "isLeader", isLeader)
	}

	return nil
}

func (db *Valkey) ShouldPerformBackup(ctx context.Context) bool {
	if !db.clusterMode {
		return true
	}

	if db.leaderElection == nil {
		db.log.Warn("cluster mode enabled but leader election not initialized")
		return false
	}

	isLeader := db.leaderElection.IsLeader()
	if !isLeader {
		db.log.Debug("not the leader, skipping backup coordination")
		return false
	}

	// Leader election considers database role: only backup if this pod is the Valkey master
	isMaster, err := db.isMaster(ctx)
	if err != nil {
		db.log.Warn("elected as backup leader but failed to check Valkey master status, skipping backup",
			"error", err)
		return false
	}

	if !isMaster {
		db.log.Info("elected as backup leader but not Valkey master, skipping backup")
		return false
	}

	db.log.Debug("elected as backup leader AND Valkey master, performing backup")
	return true
}

func (db *Valkey) isMaster(ctx context.Context) (bool, error) {
	info, err := db.client.Info(ctx, "replication").Result()
	if err != nil {
		return false, fmt.Errorf("unable to get database info %w", err)
	}

	if strings.Contains(info, "role:master") {
		db.log.Info("this is database master")
		return true, nil
	}

	db.log.Debug("this is a replica, not master")
	return false, nil
}

func getPassword(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

// Close gracefully shuts down the Valkey database connection and leader election
func (db *Valkey) Close() error {
	if db.leaderElectionCancel != nil {
		db.log.Info("stopping leader election")
		db.leaderElectionCancel()
	}

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
