package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/backup"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/backup/providers/gcp"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/database/postgres"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/database/rethinkdb"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/initializer"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/probe"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/signals"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-pod/backup-restore-sidecar/cmd/internal/wait"
	"github.com/metal-pod/v"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	moduleName = "backup-restore-sidecar"

	logLevelFlg = "log-level"

	backupProviderFlg = "backup-provider"

	serverAddrFlg = "initializer-endpoint"

	bindAddrFlg = "bind-addr"
	portFlg     = "port"
	basePathFlg = "base-path"

	databaseFlg = "db"

	postgresUserFlg     = "postgres-user"
	postgresHostFlg     = "postgres-host"
	postgresPasswordFlg = "postgres-password"
	postgresPortFlg     = "postgres-port"

	rethinkDBPasswordFileFlg = "rethinkdb-passwordfile"
	rethinkDBURLFlg          = "rethinkdb-url"

	backupIntervalFlg = "backup-interval"

	objectsToKeepFlg = "object-max-keep"
	objectPrefixFlg  = "object-prefix"

	gcpBucketNameFlg     = "gcp-bucket-name"
	gcpBucketLocationFlg = "gcp-bucket-location"
	gcpProjectFlg        = "gcp-project"
)

var (
	logger *zap.SugaredLogger
	db     database.Database
	bp     providers.BackupProvider
	stop   <-chan struct{}
)

var rootCmd = &cobra.Command{
	Use:     moduleName,
	Short:   "a backup restore sidecar for databases managed in K8s",
	Version: v.V.String(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initLogging()
		initConfig()
		initSignalHandlers()
		initDatabase()
		initBackupProvider()
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "starts the sidecar",
	Long:  "the initializer will prepare starting the database. if there is no data or corrupt data, it checks whether there is a backup available and restore it prior to running allow running the database. The sidecar will then wait until the database is available and then take backups periodically",

	Run: func(cmd *cobra.Command, args []string) {
		addr := fmt.Sprintf("%s:%d", viper.GetString(bindAddrFlg), viper.GetInt(portFlg))
		backupInterval := utils.MustParseTimeInterval(viper.GetString(backupIntervalFlg))
		initializer.New(logger.Named("initializer"), addr, db, bp).Start(stop)
		probe.Start(logger.Named("probe"), db, stop)
		backup.Start(logger.Named("backup"), backupInterval, db, bp, stop)
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "restores a specific backup manually",
	Run: func(cmd *cobra.Command, args []string) {
		version := &providers.BackupVersion{
			Version: args[0],
		}
		initializer.New(logger.Named("initializer"), "", db, bp).Restore(version, stop)
	},
}

var restoreListCmd = &cobra.Command{
	Use:     "list-versions",
	Aliases: []string{"ls"},
	Short:   "lists available backups",
	RunE: func(cmd *cobra.Command, args []string) error {
		versions, err := bp.ListBackups()
		if err != nil {
			return err
		}
		backups := versions.List()
		versions.Sort(backups, false)
		var data [][]string
		for _, b := range backups {
			data = append(data, []string{b.Date.String(), b.Name, b.Version})
		}
		p := utils.NewTablePrinter()
		p.Print([]string{"Data", "Name", "Version"}, data)
		return nil
	},
}

var waitCmd = &cobra.Command{
	Use:   "wait",
	Short: "waits for the initializer to be done",
	Run: func(cmd *cobra.Command, args []string) {
		wait.Start(logger.Named("wait"), viper.GetString(serverAddrFlg), stop)
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.Error("failed executing root command", "error", err)
	}
}

func init() {
	rootCmd.AddCommand(startCmd, waitCmd, restoreCmd)

	rootCmd.PersistentFlags().StringP(logLevelFlg, "", "info", "sets the application log level")

	err := viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		logger.Fatal("unable to construct root command:%v", err)
	}

	startCmd.Flags().StringP(bindAddrFlg, "", "127.0.0.1", "the bind addr of the api server")
	startCmd.Flags().IntP(portFlg, "", 8000, "the port to serve on")

	startCmd.Flags().StringP(databaseFlg, "", "", "the kind of the database [postgres|rethinkdb]")

	startCmd.Flags().StringP(postgresUserFlg, "", "postgres", "the postgres database user (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresHostFlg, "", "localhost", "the postgres database address (will be used when db is postgres)")
	startCmd.Flags().IntP(postgresPortFlg, "", 5432, "the postgres database port (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresPasswordFlg, "", "", "the postgres database password (will be used when db is postgres)")

	startCmd.Flags().StringP(rethinkDBURLFlg, "", "localhost:28015", "the rethinkdb database url (will be used when db is rethinkdb)")
	startCmd.Flags().StringP(rethinkDBPasswordFileFlg, "", "", "the rethinkdb database password file path (will be used when db is rethinkdb)")

	startCmd.Flags().StringP(backupProviderFlg, "", "gcp", "the name of the backup provider [gcp]")
	startCmd.Flags().StringP(backupIntervalFlg, "", "3m", "the timed interval in which to take backups integer and optional time quantity (s|m|h)")

	startCmd.Flags().IntP(objectsToKeepFlg, "", constants.DefaultObjectsToKeep, "the number of objects to keep at the cloud provider bucket")
	startCmd.Flags().StringP(objectPrefixFlg, "", "", "the prefix to store the object in the cloud provider bucket")

	startCmd.Flags().StringP(gcpBucketNameFlg, "", "", "the name of the gcp backup bucket")
	startCmd.Flags().StringP(gcpBucketLocationFlg, "", "", "the location of the gcp backup bucket")
	startCmd.Flags().StringP(gcpProjectFlg, "", "", "the project id to place the gcp backup bucket in")

	err = viper.BindPFlags(startCmd.Flags())
	if err != nil {
		logger.Error("unable to construct initializer command:%v", err)
	}

	waitCmd.Flags().StringP(serverAddrFlg, "", "http://127.0.0.1:8000/", "the url of the initializer server")

	err = viper.BindPFlags(waitCmd.Flags())
	if err != nil {
		logger.Fatal("unable to construct wait command:%v", err)
	}

	restoreCmd.AddCommand(restoreListCmd)
}

func initConfig() {
	viper.SetEnvPrefix("BACKUP_RESTORE_SIDECAR")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
}

func initLogging() {
	level := zap.InfoLevel

	if viper.IsSet(logLevelFlg) {
		err := level.UnmarshalText([]byte(viper.GetString(logLevelFlg)))
		if err != nil {
			log.Fatalf("can't initialize zap logger: %v", err)
		}
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(level)

	l, err := cfg.Build()
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}
	defer l.Sync()

	logger = l.Sugar()
}

func initSignalHandlers() {
	stop = signals.SetupSignalHandler()
}

func initDatabase() error {
	dbString := viper.GetString(databaseFlg)
	switch dbString {
	case "postgres":
		db = postgres.New(
			logger.Named("postgres"),
			viper.GetString(postgresHostFlg),
			viper.GetInt(postgresPortFlg),
			viper.GetString(postgresUserFlg),
			viper.GetString(postgresPasswordFlg),
		)
	case "rethinkdb":
		db = rethinkdb.New(
			logger.Named("rethinkdb"),
			viper.GetString(rethinkDBURLFlg),
			viper.GetString(rethinkDBPasswordFileFlg),
		)
	default:
		return fmt.Errorf("unsupported database type: %s", dbString)
	}
	logger.Infow("initialized database adapter", "type", dbString)
	return nil
}

func initBackupProvider() error {
	bpString := viper.GetString(backupProviderFlg)
	var err error
	switch bpString {
	case "gcp":
		bp, err = gcp.New(
			logger.Named("backup"),
			&gcp.BackupProviderConfigGCP{
				ObjectPrefix:   viper.GetString(objectPrefixFlg),
				ObjectsToKeep:  viper.GetInt64(objectsToKeepFlg),
				ProjectID:      viper.GetString(gcpProjectFlg),
				BucketName:     viper.GetString(gcpBucketNameFlg),
				BucketLocation: viper.GetString(gcpBucketLocationFlg),
			},
		)
		if err != nil {
			return fmt.Errorf("error initializing backup provider: %s", err)
		}
	default:
		return fmt.Errorf("unsupported backup provider type: %s", bpString)
	}
	logger.Infow("initialized backup provider", "type", bpString)
	return nil
}
