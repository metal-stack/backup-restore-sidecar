package valkey

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/afero"
)

const (
	valkeyDumpFile = "dump.rdb"
	nodeConfFile   = "nodes.conf"
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

	if clusterMode {
		v.client = redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: getPassword(password),
		})
		//nodes := v.getClusterNodes()
		//v.clusterClient = redis.NewClusterClient(&redis.ClusterOptions{
		//	Addrs:    nodes,
		//	Password: getPassword(password),
		//})
	} else {
		v.client = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: getPassword(password),
		})
	}
	return v, nil
}

func (db *Valkey) initializeClusterClient(ctx context.Context) error {
	if !db.clusterMode {
		return nil
	}

	if _, err := os.Stat("/data/nodes.conf"); os.IsNotExist(err) {
		leader := db.determineLeader(ctx)
		hostname, _ := os.Hostname()
		if leader == hostname {
			db.log.Info("initializing cluster, leader ->", "hostname", hostname)

			if err := db.waitForAllNodes(ctx); err != nil {
				return err
			}

			if err := db.createCluster(ctx); err != nil {
				return err
			}
		} else {
			db.log.Info("Not the leader, waiting for cluster formation", "leader", leader)
		}
	}

	if db.clusterClient == nil {
		nodes := db.getClusterNodes()
		db.clusterClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    nodes,
			Password: db.password,
		})
		db.log.Info("cluster client initialized", "nodes", nodes)
	}

	return nil
}

func (db *Valkey) initializeCluster(ctx context.Context) error {
	if !db.clusterMode {
		return nil
	}

	if _, err := os.Stat("/data/nodes.conf"); os.IsNotExist(err) {
		leader := db.determineLeader(ctx)
		hostname, _ := os.Hostname()
		if leader == hostname {
			db.log.Info("initializing cluster, leader ->", "hostname", hostname)
			return db.createCluster(ctx)
		} else {
			db.log.Info("Not the leader, waiting for cluster formation", "leader", leader)
		}
	}
	if db.clusterClient == nil {
		return db.initializeClusterClient(ctx)
	}
	return nil
}

func (db *Valkey) waitForAllNodes(ctx context.Context) error {
	nodes := db.getClusterNodes()

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	statefulsetName := os.Getenv("STATEFULSET_NAME")
	if statefulsetName == "" {
		statefulsetName = "valkey"
	}

	db.log.Info("waiting for all nodes to be up", "nodes", nodes)

	for _, node := range nodes {
		for attempt := 0; attempt < 50; attempt++ {
			testClient := redis.NewClient(&redis.Options{
				Addr:        node,
				DialTimeout: 2 * time.Second,
			})
			_, err := testClient.Ping(ctx).Result()
			testClient.Close()

			if err == nil {
				db.log.Info("node is up", "node", node)
				break
			}
			if attempt == 49 {
				return fmt.Errorf("node %s did not get ready", node)
			}
			time.Sleep(5 * time.Second)
		}
	}
	return nil
}

func (db *Valkey) createCluster(ctx context.Context) error {
	args := []string{"--cluster", "create"}
	args = append(args, db.getClusterNodes()...)
	args = append(args, "--cluster-replicas", "0", "--cluster-yes")

	db.log.Info("creating cluster", "command", "valkey-cli", "args", args)

	output, err := db.executor.ExecuteCommandWithOutput(ctx, "valkey-cli", args)
	if err != nil {
		return fmt.Errorf("error creating cluster: %w , output: %s", err, output)
	}

	db.log.Info("cluster created successfully", "output", output)

	if err := db.initializeClusterClient(ctx); err != nil {
		return fmt.Errorf("error initializing cluster client: %w", err)
	}

	return nil
}

func (db *Valkey) determineLeader(ctx context.Context) string {
	hostname, _ := os.Hostname()

	for i, node := range db.getClusterNodes() {
		testClient := redis.NewClient(&redis.Options{
			//Addr: fmt.Sprintf("%s:6379", node),
			Addr: node,
		})
		defer func(testClient *redis.Client) {
			err := testClient.Close()
			if err != nil {

			}
		}(testClient)

		_, err := testClient.Ping(ctx).Result()
		if err == nil {
			//return fmt.Sprintf("valkey-%d", i)
			return fmt.Sprintf("%s-%d", db.statefulsetName, i)
		}
	}
	return hostname
}

func (db *Valkey) Probe(ctx context.Context) error {
	if err := db.initializeCluster(ctx); err != nil {
		return err
	}
	if db.clusterMode {
		if db.clusterClient == nil {
			if err := db.initializeClusterClient(ctx); err != nil {
				return fmt.Errorf("error initializing cluster client: %w", err)
			}
		}
		_, err := db.clusterClient.Ping(ctx).Result()
		return err
	} else {
		_, err := db.client.Ping(ctx).Result()
		return err
	}
}

func (db *Valkey) isMaster(ctx context.Context) (bool, error) {
	var info string
	var err error
	if db.clusterMode {
		if db.clusterClient == nil {
			return false, fmt.Errorf("cluster client not initialized")
		}
		info, err = db.clusterClient.Info(ctx, "replication").Result()
	} else {
		info, err = db.clusterClient.Info(ctx, "replication").Result()
	}
	if err != nil {
		return false, fmt.Errorf("unable to get database info %w", err)
	}

	if strings.Contains(info, "role:master") {
		db.log.Info("this is database master")
		return true, nil
	}
	return false, nil
}

func (db *Valkey) getClusterNodes() []string {
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	nodes := make([]string, db.clusterSize)
	for i := 0; i < db.clusterSize; i++ {
		nodes[i] = fmt.Sprintf(
			"%s-%d.%s.%s.svc.cluster.local:6379",
			db.statefulsetName,
			i,
			db.statefulsetName,
			namespace,
		)
	}
	return nodes
}

func getPassword(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}
