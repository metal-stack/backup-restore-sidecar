package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/gcp"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/local"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/postgres"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/rethinkdb"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/initializer"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/probe"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/signals"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/wait"
	"github.com/metal-stack/v"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	moduleName  = "backup-restore-sidecar"
	cfgFileType = "yaml"

	// Flags
	logLevelFlg = "log-level"

	serverAddrFlg = "initializer-endpoint"

	bindAddrFlg = "bind-addr"
	portFlg     = "port"

	databaseFlg        = "db"
	databaseDatadirFlg = "db-data-directory"

	postgresUserFlg     = "postgres-user"
	postgresHostFlg     = "postgres-host"
	postgresPasswordFlg = "postgres-password"
	postgresPortFlg     = "postgres-port"

	rethinkDBPasswordFileFlg = "rethinkdb-passwordfile"
	rethinkDBURLFlg          = "rethinkdb-url"

	backupProviderFlg     = "backup-provider"
	backupCronScheduleFlg = "backup-cron-schedule"

	objectsToKeepFlg = "object-max-keep"
	objectPrefixFlg  = "object-prefix"

	localBackupPathFlg = "local-provider-backup-path"

	gcpBucketNameFlg     = "gcp-bucket-name"
	gcpBucketLocationFlg = "gcp-bucket-location"
	gcpProjectFlg        = "gcp-project"
)

var (
	cfgFile string
	logger  *zap.SugaredLogger
	db      database.Database
	bp      providers.BackupProvider
	stop    <-chan struct{}
)

var rootCmd = &cobra.Command{
	Use:     moduleName,
	Short:   "a backup restore sidecar for databases managed in K8s",
	Version: v.V.String(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initLogging()
		initConfig()
		initSignalHandlers()
		if err := initDatabase(); err != nil {
			return err
		}
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "starts the sidecar",
	Long:  "the initializer will prepare starting the database. if there is no data or corrupt data, it checks whether there is a backup available and restore it prior to running allow running the database. The sidecar will then wait until the database is available and then take backups periodically",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return initBackupProvider()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := fmt.Sprintf("%s:%d", viper.GetString(bindAddrFlg), viper.GetInt(portFlg))
		initializer.New(logger.Named("initializer"), addr, db, bp).Start(stop)
		if err := probe.Start(logger.Named("probe"), db, stop); err != nil {
			return err
		}
		metrics := metrics.New()
		metrics.Start(logger.Named("metrics"))
		return backup.Start(logger.Named("backup"), viper.GetString(backupCronScheduleFlg), db, bp, metrics, stop)
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "restores a specific backup manually",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return initBackupProvider()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("no version argument given")
		}
		versions, err := bp.ListBackups()
		if err != nil {
			return err
		}
		version, err := versions.Get(args[0])
		if err != nil {
			return err
		}

		return initializer.New(logger.Named("initializer"), "", db, bp).Restore(version)
	},
}

var restoreListCmd = &cobra.Command{
	Use:     "list-versions",
	Aliases: []string{"ls"},
	Short:   "lists available backups",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return initBackupProvider()
	},
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
	RunE: func(cmd *cobra.Command, args []string) error {
		return wait.Start(logger.Named("wait"), viper.GetString(serverAddrFlg), stop)
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.Fatalw("failed executing root command", "error", err)
	}
}

func init() {
	rootCmd.AddCommand(startCmd, waitCmd, restoreCmd)

	rootCmd.PersistentFlags().StringP(logLevelFlg, "", "info", "sets the application log level")
	rootCmd.PersistentFlags().StringP(databaseFlg, "", "", "the kind of the database [postgres|rethinkdb]")
	rootCmd.PersistentFlags().StringP(databaseDatadirFlg, "", "", "the directory where the database stores its data in")

	err := viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		fmt.Printf("unable to construct root command: %v", err)
		os.Exit(1)
	}

	startCmd.Flags().StringP(bindAddrFlg, "", "127.0.0.1", "the bind addr of the api server")
	startCmd.Flags().IntP(portFlg, "", 8000, "the port to serve on")

	startCmd.Flags().StringP(postgresUserFlg, "", "postgres", "the postgres database user (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresHostFlg, "", "localhost", "the postgres database address (will be used when db is postgres)")
	startCmd.Flags().IntP(postgresPortFlg, "", 5432, "the postgres database port (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresPasswordFlg, "", "", "the postgres database password (will be used when db is postgres)")

	startCmd.Flags().StringP(rethinkDBURLFlg, "", "localhost:28015", "the rethinkdb database url (will be used when db is rethinkdb)")
	startCmd.Flags().StringP(rethinkDBPasswordFileFlg, "", "", "the rethinkdb database password file path (will be used when db is rethinkdb)")

	startCmd.Flags().StringP(backupProviderFlg, "", "", "the name of the backup provider [gcp|local]")
	startCmd.Flags().StringP(backupCronScheduleFlg, "", "*/3 * * * *", "cron schedule for taking backups periodically")

	startCmd.Flags().IntP(objectsToKeepFlg, "", constants.DefaultObjectsToKeep, "the number of objects to keep at the cloud provider bucket")
	startCmd.Flags().StringP(objectPrefixFlg, "", "", "the prefix to store the object in the cloud provider bucket")

	startCmd.Flags().StringP(gcpBucketNameFlg, "", "", "the name of the gcp backup bucket")
	startCmd.Flags().StringP(gcpBucketLocationFlg, "", "", "the location of the gcp backup bucket")
	startCmd.Flags().StringP(gcpProjectFlg, "", "", "the project id to place the gcp backup bucket in")

	err = viper.BindPFlags(startCmd.Flags())
	if err != nil {
		fmt.Printf("unable to construct initializer command: %v", err)
		os.Exit(1)
	}

	waitCmd.Flags().StringP(serverAddrFlg, "", "http://127.0.0.1:8000/", "the url of the initializer server")

	err = viper.BindPFlags(waitCmd.Flags())
	if err != nil {
		fmt.Printf("unable to construct wait command: %v", err)
		os.Exit(1)
	}

	restoreCmd.AddCommand(restoreListCmd)
}

func initConfig() {
	viper.SetEnvPrefix("BACKUP_RESTORE_SIDECAR")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	viper.SetConfigType(cfgFileType)

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			logger.Fatalw("config file path set explicitly, but unreadable", "error", err)
		}
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath("/etc/" + moduleName)
		viper.AddConfigPath("$HOME/." + moduleName)
		viper.AddConfigPath(".")
		if err := viper.ReadInConfig(); err != nil {
			usedCfg := viper.ConfigFileUsed()
			if usedCfg != "" {
				logger.Fatalw("config file unreadable", "config-file", usedCfg, "error", err)
			}
		}
	}

	usedCfg := viper.ConfigFileUsed()
	if usedCfg != "" {
		logger.Infow("read config file", "config-file", usedCfg)
	}
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

	logger = l.Sugar()
}

func initSignalHandlers() {
	stop = signals.SetupSignalHandler()
}

func initDatabase() error {
	datadir := viper.GetString(databaseDatadirFlg)
	if datadir == "" {
		return fmt.Errorf("database data directory (%s) must be set", databaseDatadirFlg)
	}

	dbString := viper.GetString(databaseFlg)
	switch dbString {
	case "postgres":
		db = postgres.New(
			logger.Named("postgres"),
			datadir,
			viper.GetString(postgresHostFlg),
			viper.GetInt(postgresPortFlg),
			viper.GetString(postgresUserFlg),
			viper.GetString(postgresPasswordFlg),
		)
	case "rethinkdb":
		db = rethinkdb.New(
			logger.Named("rethinkdb"),
			datadir,
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
	case "local":
		bp, err = local.New(
			logger.Named("backup"),
			&local.BackupProviderConfigLocal{
				LocalBackupPath: viper.GetString(localBackupPathFlg),
				ObjectsToKeep:   viper.GetInt64(objectsToKeepFlg),
			},
		)
	default:
		return fmt.Errorf("unsupported backup provider type: %s", bpString)
	}
	if err != nil {
		return fmt.Errorf("error initializing backup provider: %s", err)
	}
	logger.Infow("initialized backup provider", "type", bpString)
	return nil
}
