package valkey

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
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

	client        *redis.Client
	clusterClient *redis.ClusterClient
	clusterMode   bool
	clusterSize   int

	statefulsetName string
	password        string

	leaderElection        *leaderelection.LeaderElection
	leaderElectionStarted bool
}

func (db *Valkey) Check(ctx context.Context) (bool, error) {
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

		if !db.leaderElection.IsLeader() {
			db.log.Info("no the leader, skipping restore. Will sync from master")
			return nil
		}

		db.log.Info("restoring from master")
		return db.performRestore(ctx, dump)
	}
	return db.performRestore(ctx, dump)
}

func (db *Valkey) performRestore(ctx context.Context, dump string) error {
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

func (db *Valkey) Upgrade(ctx context.Context) error {
	return nil
}

func (db *Valkey) Backup(ctx context.Context) error {
	isMaster, err := db.isMaster(ctx)
	if err != nil {
		return err
	}
	if !isMaster {
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

	var saveErr error
	if db.clusterMode {
		if db.clusterClient == nil {
			return fmt.Errorf("cluster client not initialized")
		}
		_, saveErr = db.clusterClient.Save(ctx).Result()
	} else {
		_, saveErr = db.client.Save(ctx).Result()
	}

	if saveErr != nil {
		return fmt.Errorf("could not create a dump: %w", saveErr)
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

func New(log *slog.Logger, datadir string, password *string, addr string,
	statefulsetName string, clusterMode bool, clusterSize int) (*Valkey, error) {
	v := &Valkey{
		log:             log,
		datadir:         datadir,
		password:        getPassword(password),
		executor:        utils.NewExecutor(log),
		clusterMode:     clusterMode,
		clusterSize:     clusterSize,
		statefulsetName: statefulsetName,
	}

	log.Info("Creating Valkey instance", "clusterMode", clusterMode, "clusterSize", clusterSize)

	v.client = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: getPassword(password),
	})
	if clusterMode {
		leaderElection, err := leaderelection.New(leaderelection.Config{
			Log:      log,
			LockName: fmt.Sprintf("valkey-backup-restore-%s", v.statefulsetName),
			OnStartedLeading: func(ctx context.Context) {
				log.Info("became leader for valkey-cluster backup/restore coordination")
			},
			OnStoppedLeading: func() {
				log.Info("stopped being leader for valkey-cluster backup/restore coordination")
			},
			OnNewLeader: func(identity string) {
				log.Info("new leader elected for valkey-cluster backup/restore coordination",
					"leader", identity)
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create leader election: %w", err)
		}
		v.leaderElection = leaderElection

		log.Info("cluster mode enabled with leader election coordination")
	}
	return v, nil
}

func (db *Valkey) Probe(ctx context.Context) error {
	if db.clusterMode && db.leaderElection != nil && !db.leaderElectionStarted {
		db.leaderElectionStarted = true
		go func() {
			if err := db.leaderElection.Start(context.Background()); err != nil {
				db.log.Error("leader election failed", "error", err)
			}
		}()

		db.log.Info("waiting for leader election to establish")
		time.Sleep(3 * time.Second)
	}

	_, err := db.client.Ping(ctx).Result()
	return err
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
