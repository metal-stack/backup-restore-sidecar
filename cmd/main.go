package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/gcp"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/local"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/s3"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/etcd"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/localfs"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/meilisearch"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/postgres"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/redis"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/database/rethinkdb"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/encryption"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/initializer"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/metrics"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/probe"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/wait"
	"github.com/metal-stack/backup-restore-sidecar/pkg/client"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/v"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

	preExecCommandsFlg  = "pre-exec-cmds"
	postExecCommandsFlg = "post-exec-cmds"

	postgresUserFlg     = "postgres-user"
	postgresHostFlg     = "postgres-host"
	postgresPasswordFlg = "postgres-password"
	postgresPortFlg     = "postgres-port"

	meilisearchURLFlg    = "meilisearch-url"
	meilisearchAPIKeyFlg = "meilisearch-apikey"

	redisAddrFlg     = "redis-addr"
	redisPasswordFlg = "redis-password"

	rethinkDBPasswordFileFlg = "rethinkdb-passwordfile"
	rethinkDBURLFlg          = "rethinkdb-url"

	etcdCaCert    = "etcd-ca-cert"
	etcdCert      = "etcd-cert"
	etcdKey       = "etcd-key"
	etcdEndpoints = "etcd-endpoints"
	etcdName      = "etcd-name"

	backupProviderFlg     = "backup-provider"
	backupCronScheduleFlg = "backup-cron-schedule"

	objectsToKeepFlg = "object-max-keep"
	objectPrefixFlg  = "object-prefix"

	localBackupPathFlg = "local-provider-backup-path"

	gcpBucketNameFlg     = "gcp-bucket-name"
	gcpBucketLocationFlg = "gcp-bucket-location"
	gcpProjectFlg        = "gcp-project"

	s3BucketNameFlg = "s3-bucket-name"
	s3RegionFlg     = "s3-region"
	s3EndpointFlg   = "s3-endpoint"
	s3AccessKeyFlg  = "s3-access-key"
	//nolint
	s3SecretKeyFlg = "s3-secret-key"

	compressionMethod = "compression-method"

	encryptionKeyFlg = "encryption-key"

	downloadOutputFlg = "output"
)

var (
	cfgFile   string
	logger    *slog.Logger
	db        database.Database
	bp        providers.BackupProvider
	encrypter *encryption.Encrypter
	stop      context.Context
)

var rootCmd = &cobra.Command{
	Use:          moduleName,
	Short:        "a backup restore sidecar for databases managed in K8s",
	Version:      v.V.String(),
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initLogging()
		initConfig()
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "starts the sidecar",
	Long:  "the initializer will prepare starting the database. if there is no data or corrupt data, it checks whether there is a backup available and restore it prior to running allow running the database. The sidecar will then wait until the database is available and then take backups periodically",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		initSignalHandlers()
		if err := initDatabase(); err != nil {
			return err
		}
		if err := initEncrypter(); err != nil {
			return err
		}
		return initBackupProvider()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, cmd := range viper.GetStringSlice(preExecCommandsFlg) {
			logger.Info("running pre-exec command", "cmd", cmd)

			executor := utils.NewExecutor(logger.WithGroup("pre-executor"))

			err := executor.ExecWithStreamingOutput(stop, cmd)
			if err != nil {
				return err
			}
		}

		addr := fmt.Sprintf("%s:%d", viper.GetString(bindAddrFlg), viper.GetInt(portFlg))

		logger.Info("starting backup-restore-sidecar", "version", v.V, "bind-addr", addr)

		comp, err := compress.New(viper.GetString(compressionMethod))
		logger.Info(comp.Extension())
		if err != nil {
			return err
		}

		metrics := metrics.New()
		metrics.Start(logger.WithGroup("metrics"))

		backuper := backup.New(&backup.BackuperConfig{
			Log:            logger.WithGroup("backup"),
			BackupSchedule: viper.GetString(backupCronScheduleFlg),
			DatabaseProber: db,
			BackupProvider: bp,
			Metrics:        metrics,
			Compressor:     comp,
			Encrypter:      encrypter,
		})

		if err := initializer.New(logger.WithGroup("initializer"), addr, db, bp, comp, metrics, viper.GetString(databaseDatadirFlg), encrypter).Start(stop, backuper); err != nil {
			return err
		}

		if err := probe.Start(stop, logger.WithGroup("probe"), db); err != nil {
			return err
		}

		return backuper.Start(stop)
	},
}

var createBackupCmd = &cobra.Command{
	Use:   "create-backup",
	Short: "create backup takes a database backup out of the regular time schedule",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return initBackupProvider()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New(context.Background(), viper.GetString(serverAddrFlg))
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}

		_, err = c.DatabaseServiceClient().CreateBackup(context.Background(), &v1.CreateBackupRequest{})
		return err
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

		c, err := client.New(context.Background(), viper.GetString(serverAddrFlg))
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}

		_, err = c.BackupServiceClient().RestoreBackup(context.Background(), &v1.RestoreBackupRequest{
			Version: args[0],
		})
		return err
	},
}

var restoreListCmd = &cobra.Command{
	Use:     "list-versions",
	Aliases: []string{"ls"},
	Short:   "lists available backups",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New(context.Background(), viper.GetString(serverAddrFlg))
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}

		backups, err := c.BackupServiceClient().ListBackups(context.Background(), &v1.ListBackupsRequest{})
		if err != nil {
			return fmt.Errorf("error listing backups: %w", err)
		}

		var data [][]string
		for _, b := range backups.GetBackups() {
			data = append(data, []string{b.GetTimestamp().AsTime().String(), b.GetName(), b.GetVersion()})
		}

		p := utils.NewTablePrinter()
		p.Print([]string{"Date", "Name", "Version"}, data)
		return nil
	},
}

var waitCmd = &cobra.Command{
	Use:   "wait",
	Short: "waits for the initializer to be done",
	PreRun: func(cmd *cobra.Command, args []string) {
		initSignalHandlers()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := wait.Start(stop, logger.WithGroup("wait"), viper.GetString(serverAddrFlg)); err != nil {
			return err
		}

		for _, cmd := range viper.GetStringSlice(postExecCommandsFlg) {
			logger.Info("running post-exec command", "cmd", cmd)
			executor := utils.NewExecutor(logger.WithGroup("post-executor"))

			err := executor.ExecWithStreamingOutput(stop, cmd)
			if err != nil {
				return err
			}
		}

		return nil
	},
}

var downloadBackupCmd = &cobra.Command{
	Use:   "download <version>",
	Short: "downloads backup without restoring",
	PreRunE: func(cm *cobra.Command, args []string) error {
		err := initEncrypter()
		if err != nil {
			return err
		}
		return initBackupProvider()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("no version argument specified")
		}

		c, err := client.New(context.Background(), viper.GetString(serverAddrFlg))
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}

		backup, err := c.BackupServiceClient().GetBackupByVersion(context.Background(), &v1.GetBackupByVersionRequest{Version: args[0]})

		if err != nil {
			return fmt.Errorf("error getting backup by version: %w", err)
		}

		output := viper.GetString(downloadOutputFlg)

		outputPath := filepath.Join(output, backup.GetBackup().GetName())
		outputFile, err := os.Open(outputPath)
		if err != nil {
			return fmt.Errorf("failed opening output file: %w", err)
		}

		reader1, writer1 := io.Pipe()

		err = bp.DownloadBackup(context.Background(), &providers.BackupVersion{Name: backup.GetBackup().GetName()}, writer1)

		if err != nil {
			return fmt.Errorf("failed downloading backup: %w", err)
		}

		if encrypter != nil {
			if encryption.IsEncrypted(backup.GetBackup().GetName()) {
				err = encrypter.Decrypt(reader1, outputFile)
				if err != nil {
					return fmt.Errorf("unable to decrypt backup: %w", err)
				}
			} else {
				logger.Info("downloading unencrypted backup with configured encryption - skipping decryption...")
			}
		}
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		if logger == nil {
			panic(err)
		}
		logger.Error("failed executing root command", "error", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(startCmd, waitCmd, restoreCmd, createBackupCmd, downloadBackupCmd)

	rootCmd.PersistentFlags().StringP(logLevelFlg, "", "info", "sets the application log level")
	rootCmd.PersistentFlags().StringP(databaseFlg, "", "", "the kind of the database [postgres|rethinkdb|etcd|meilisearch|redis|keydb|localfs]")
	rootCmd.PersistentFlags().StringP(databaseDatadirFlg, "", "", "the directory where the database stores its data in")

	err := viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		fmt.Printf("unable to construct root command: %v", err)
		os.Exit(1)
	}

	startCmd.Flags().StringP(bindAddrFlg, "", "127.0.0.1", "the bind addr of the api server")
	startCmd.Flags().IntP(portFlg, "", 8000, "the port to serve on")

	startCmd.Flags().StringSlice(preExecCommandsFlg, nil, "runs given commands prior to executing the backup-restore-sidecar functionality")

	startCmd.Flags().StringP(postgresUserFlg, "", "postgres", "the postgres database user (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresHostFlg, "", "127.0.0.1", "the postgres database address (will be used when db is postgres)")
	startCmd.Flags().IntP(postgresPortFlg, "", 5432, "the postgres database port (will be used when db is postgres)")
	startCmd.Flags().StringP(postgresPasswordFlg, "", "", "the postgres database password (will be used when db is postgres)")

	startCmd.Flags().StringP(rethinkDBURLFlg, "", "localhost:28015", "the rethinkdb database url (will be used when db is rethinkdb)")
	startCmd.Flags().StringP(rethinkDBPasswordFileFlg, "", "", "the rethinkdb database password file path (will be used when db is rethinkdb)")

	startCmd.Flags().StringP(etcdCaCert, "", "", "path of the ETCD CA file (optional)")
	startCmd.Flags().StringP(etcdCert, "", "", "path of the ETCD Cert file (optional)")
	startCmd.Flags().StringP(etcdKey, "", "", "path of the ETCD private key file (optional)")
	startCmd.Flags().StringP(etcdEndpoints, "", "http://localhost:2379", "URL to connect to ETCD with V3 protocol (optional)")
	startCmd.Flags().StringP(etcdName, "", "", "name of the ETCD to connect to (optional)")

	startCmd.Flags().StringP(backupProviderFlg, "", "", "the name of the backup provider [gcp|s3|local]")
	startCmd.Flags().StringP(backupCronScheduleFlg, "", "*/3 * * * *", "cron schedule for taking backups periodically")

	startCmd.Flags().IntP(objectsToKeepFlg, "", constants.DefaultObjectsToKeep, "the number of objects to keep at the cloud provider bucket")
	startCmd.Flags().StringP(objectPrefixFlg, "", "", "the prefix to store the object in the cloud provider bucket")

	startCmd.Flags().StringP(gcpBucketNameFlg, "", "", "the name of the gcp backup bucket")
	startCmd.Flags().StringP(gcpBucketLocationFlg, "", "", "the location of the gcp backup bucket")
	startCmd.Flags().StringP(gcpProjectFlg, "", "", "the project id to place the gcp backup bucket in")

	startCmd.Flags().StringP(s3BucketNameFlg, "", "", "the name of the s3 backup bucket")
	startCmd.Flags().StringP(s3RegionFlg, "", "", "the region of the s3 backup bucket")
	startCmd.Flags().StringP(s3EndpointFlg, "", "", "the url to the s3 endpoint")
	startCmd.Flags().StringP(s3AccessKeyFlg, "", "", "the s3 access-key-id")
	startCmd.Flags().StringP(s3SecretKeyFlg, "", "", "the s3 secret-key-id")

	startCmd.Flags().StringP(compressionMethod, "", "targz", "the compression method to use to compress the backups (tar|targz|tarlz4)")

	startCmd.Flags().StringP(encryptionKeyFlg, "", "", "the encryption key for aes")

	err = viper.BindPFlags(startCmd.Flags())
	if err != nil {
		fmt.Printf("unable to construct initializer command: %v", err)
		os.Exit(1)
	}

	waitCmd.Flags().StringP(serverAddrFlg, "", "http://127.0.0.1:8000/", "the url of the initializer server")

	waitCmd.Flags().StringSlice(postExecCommandsFlg, nil, "runs given commands after finished waiting for the backup-restore-sidecar's initializer (typically used for starting the database)")

	err = viper.BindPFlags(waitCmd.Flags())
	if err != nil {
		fmt.Printf("unable to construct wait command: %v", err)
		os.Exit(1)
	}

	restoreCmd.AddCommand(restoreListCmd)

	downloadBackupCmd.Flags().StringP(downloadOutputFlg, "o", constants.DownloadDir, "the target directory for the downloaded backup")
	err = viper.BindPFlags(downloadBackupCmd.Flags())
	if err != nil {
		fmt.Printf("unable to construct download command: %v", err)
		os.Exit(1)
	}
}

func initConfig() {
	viper.SetEnvPrefix("BACKUP_RESTORE_SIDECAR")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	viper.SetConfigType(cfgFileType)

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			logger.Error("config file path set explicitly, but unreadable", "error", err)
			panic(err)
		}
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath("/etc/" + moduleName)
		viper.AddConfigPath("$HOME/." + moduleName)
		viper.AddConfigPath(".")
		if err := viper.ReadInConfig(); err != nil {
			usedCfg := viper.ConfigFileUsed()
			if usedCfg != "" {
				logger.Error("config file unreadable", "config-file", usedCfg, "error", err)
				panic(err)
			}
		}
	}

	usedCfg := viper.ConfigFileUsed()
	if usedCfg != "" {
		logger.Info("read config file", "config-file", usedCfg)
	}
}

func initLogging() {

	level := slog.LevelInfo
	if viper.IsSet(logLevelFlg) {
		switch strings.ToLower(viper.GetString(logLevelFlg)) {
		case "info":
			level = slog.LevelInfo
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			panic(fmt.Errorf("unknown log level:%q given", viper.GetString(logLevelFlg)))
		}
	}

	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	logger = slog.New(jsonHandler)
}

func initSignalHandlers() {
	// don't need to store
	stop, _ = signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
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
			logger.WithGroup("postgres"),
			datadir,
			viper.GetString(postgresHostFlg),
			viper.GetInt(postgresPortFlg),
			viper.GetString(postgresUserFlg),
			viper.GetString(postgresPasswordFlg),
		)
	case "rethinkdb":
		db = rethinkdb.New(
			logger.WithGroup("rethinkdb"),
			datadir,
			viper.GetString(rethinkDBURLFlg),
			viper.GetString(rethinkDBPasswordFileFlg),
		)
	case "etcd":
		db = etcd.New(
			logger.WithGroup("etcd"),
			datadir,
			viper.GetString(etcdCaCert),
			viper.GetString(etcdCert),
			viper.GetString(etcdKey),
			viper.GetString(etcdEndpoints),
			viper.GetString(etcdName),
		)
	case "meilisearch":
		var err error
		db, err = meilisearch.New(
			logger.WithGroup("meilisearch"),
			datadir,
			viper.GetString(meilisearchURLFlg),
			viper.GetString(meilisearchAPIKeyFlg),
		)
		if err != nil {
			return err
		}
	case "redis", "keydb":
		var err error
		var password string
		if viper.IsSet(redisPasswordFlg) {
			password = viper.GetString(redisPasswordFlg)
		}
		db, err = redis.New(
			logger.WithGroup("redis"),
			datadir,
			viper.GetString(redisAddrFlg),
			&password,
		)
		if err != nil {
			return err
		}
	case "localfs":
		db = localfs.New(
			logger.WithGroup("localfs"),
			datadir,
		)
	default:
		return fmt.Errorf("unsupported database type: %s", dbString)
	}

	logger.Info("initialized database adapter", "type", dbString)

	return nil
}

func initEncrypter() error {
	var err error
	key := viper.GetString(encryptionKeyFlg)
	if key != "" {
		encrypter, err = encryption.New(logger.WithGroup("encrypter"), &encryption.EncrypterConfig{Key: key})
		if err != nil {
			return fmt.Errorf("unable to initialize encrypter: %w", err)
		}
		logger.Info("initialized encrypter")
	}
	return nil
}

func initBackupProvider() error {
	bpString := viper.GetString(backupProviderFlg)
	var err error
	switch bpString {
	case "gcp":
		bp, err = gcp.New(
			context.Background(),
			logger.WithGroup("backup"),
			&gcp.BackupProviderConfigGCP{
				ObjectPrefix:   viper.GetString(objectPrefixFlg),
				ObjectsToKeep:  viper.GetInt64(objectsToKeepFlg),
				ProjectID:      viper.GetString(gcpProjectFlg),
				BucketName:     viper.GetString(gcpBucketNameFlg),
				BucketLocation: viper.GetString(gcpBucketLocationFlg),
			},
		)
	case "s3":
		bp, err = s3.New(
			logger.WithGroup("backup"),
			&s3.BackupProviderConfigS3{
				ObjectPrefix:  viper.GetString(objectPrefixFlg),
				ObjectsToKeep: viper.GetInt64(objectsToKeepFlg),
				Region:        viper.GetString(s3RegionFlg),
				BucketName:    viper.GetString(s3BucketNameFlg),
				Endpoint:      viper.GetString(s3EndpointFlg),
				AccessKey:     viper.GetString(s3AccessKeyFlg),
				SecretKey:     viper.GetString(s3SecretKeyFlg),
			},
		)
	case "local":
		bp, err = local.New(
			logger.WithGroup("backup"),
			&local.BackupProviderConfigLocal{
				LocalBackupPath: viper.GetString(localBackupPathFlg),
				ObjectsToKeep:   viper.GetInt64(objectsToKeepFlg),
			},
		)
	default:
		return fmt.Errorf("unsupported backup provider type: %s", bpString)
	}
	if err != nil {
		return fmt.Errorf("error initializing backup provider: %w", err)
	}
	logger.Info("initialized backup provider", "type", bpString)
	return nil
}
