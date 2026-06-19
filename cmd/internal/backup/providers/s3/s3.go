package s3

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/afero"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
)

const (
	defaultBackupName     = "db"
	checksumWhenSupported = "when_supported"
	checksumWhenRequired  = "when_required"
)

// BackupProviderS3 implements the backup provider interface for S3
type BackupProviderS3 struct {
	fs     afero.Fs
	log    *slog.Logger
	c      *s3.Client
	config *BackupProviderConfigS3
	suffix string
}

// BackupProviderConfigS3 provides configuration for the BackupProviderS3
type BackupProviderConfigS3 struct {
	BucketName                 string
	Endpoint                   string
	Region                     string
	AccessKey                  string
	SecretKey                  string
	BackupName                 string
	InsecureSkipVerify         *bool
	TrustedCaCert              *string
	ObjectPrefix               string
	ObjectsToKeep              int32
	ObjectDaysToKeep           int32
	FS                         afero.Fs
	Suffix                     string
	RequestChecksumCalculation *string
}

func (c *BackupProviderConfigS3) validate() error {
	if c.BucketName == "" {
		return errors.New("s3 bucket name must not be empty")
	}
	if c.Endpoint == "" {
		return errors.New("s3 endpoint must not be empty")
	}
	if c.AccessKey == "" {
		return errors.New("s3 accesskey must not be empty")
	}
	if c.SecretKey == "" {
		return errors.New("s3 secretkey must not be empty")
	}
	if c.InsecureSkipVerify != nil && *c.InsecureSkipVerify && c.TrustedCaCert != nil {
		return errors.New("s3 skip verify certificate and trusted CA certificate cannot be set at the same time")
	}
	if c.RequestChecksumCalculation != nil {
		switch *c.RequestChecksumCalculation {
		case checksumWhenRequired, checksumWhenSupported:
		default:
			return fmt.Errorf("s3 request checksum calculation must be %q or %q", checksumWhenRequired, checksumWhenSupported)
		}
	}
	return nil
}

// New returns a S3 backup provider
func New(log *slog.Logger, cfg *BackupProviderConfigS3) (*BackupProviderS3, error) {
	if cfg == nil {
		return nil, errors.New("s3 backup provider requires a provider config")
	}

	if cfg.ObjectsToKeep == 0 {
		cfg.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if cfg.ObjectDaysToKeep == 0 {
		cfg.ObjectDaysToKeep = constants.DefaultObjectDaysToKeep
	}
	if cfg.BackupName == "" {
		cfg.BackupName = defaultBackupName
	}
	if cfg.FS == nil {
		cfg.FS = afero.NewOsFs()
	}

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	httpClient := http.DefaultClient
	if cfg.InsecureSkipVerify != nil && *cfg.InsecureSkipVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	if cfg.TrustedCaCert != nil {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(*cfg.TrustedCaCert))
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				InsecureSkipVerify: false,
				MinVersion:         tls.VersionTLS13,
			},
		}
	}

	s3Cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
		config.WithRegion(cfg.Region),
		config.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(s3Cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
		if cfg.RequestChecksumCalculation != nil {
			switch *cfg.RequestChecksumCalculation {
			case checksumWhenRequired:
				o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			case checksumWhenSupported:
				o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenSupported
			}
		}
	})

	return &BackupProviderS3{
		c:      client,
		config: cfg,
		log:    log,
		fs:     cfg.FS,
		suffix: cfg.Suffix,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderS3) EnsureBackupBucket(ctx context.Context) error {
	// some s3 storage implementations do not properly return the already exists or already owned by you
	// error codes. therefore, we check if the bucket already exists in the backend by listing them first.
	alreadyExists := false

	buckets, err := b.c.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("unable to list backup buckets: %w", err)
	}

	if slices.ContainsFunc(buckets.Buckets, func(bucket types.Bucket) bool {
		if bucket.Name == nil {
			return false
		}

		return *bucket.Name == b.config.BucketName
	}) {
		alreadyExists = true
	}

	if !alreadyExists {
		// create bucket
		_, err = b.c.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(b.config.BucketName),
		})
		if err != nil {
			var (
				bucketAlreadyExists     *types.BucketAlreadyExists
				bucketAlreadyOwnerByYou *types.BucketAlreadyOwnedByYou
			)
			if !errors.As(err, &bucketAlreadyExists) && !errors.As(err, &bucketAlreadyOwnerByYou) {
				return err
			}
		}
	}

	// enable versioning
	_, err = b.c.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(b.config.BucketName),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	})
	if err != nil {
		return err
	}

	lifecycleRuleID := aws.String(b.config.ObjectPrefix + "-backup-restore-lifecycle")

	rules := []types.LifecycleRule{
		{
			NoncurrentVersionExpiration: &types.NoncurrentVersionExpiration{
				NewerNoncurrentVersions: &b.config.ObjectsToKeep,
				NoncurrentDays:          &b.config.ObjectDaysToKeep,
			},
			Status: types.ExpirationStatusEnabled,
			ID:     lifecycleRuleID,
			Filter: &types.LifecycleRuleFilter{
				Prefix: aws.String(b.config.ObjectPrefix + "/"),
			},
		},
	}

	// get existing lifecycle configuration
	bucketLifecycleConfiguration, err := b.c.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(b.config.BucketName),
	})

	if bucketLifecycleConfiguration != nil {
		if err != nil {
			return err
		}
		for _, r := range bucketLifecycleConfiguration.Rules {
			if r.ID != nil && lifecycleRuleID != nil {
				if *r.ID != *lifecycleRuleID {
					rules = append(rules, r)
				}
			}
		}
	}

	// add lifecycle policy
	_, err = b.c.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(b.config.BucketName),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: rules,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// CleanupBackups cleans up backups according to the given backup cleanup policy at the backup provider
func (b *BackupProviderS3) CleanupBackups(_ context.Context) error {
	// nothing to do here, done with lifecycle rules
	return nil
}

// DownloadBackup downloads the given backup version to the specified folder
func (b *BackupProviderS3) DownloadBackup(ctx context.Context, version *providers.BackupVersion, writer io.Writer) error {
	bucket := aws.String(b.config.BucketName)

	downloader := manager.NewDownloader(b.c)
	// we need to download the backup sequentially since we fake the download with a io.Writer instead of io.WriterAt
	downloader.Concurrency = 1

	b.log.Info("downloading", "object", version.Name, "get", version.Version)

	streamWriter := utils.NewSequentialWriterAt(writer)
	_, err := downloader.Download(
		ctx,
		streamWriter,
		&s3.GetObjectInput{
			Bucket:    bucket,
			Key:       &version.Name,
			VersionId: &version.Version,
		})
	if err != nil {
		return err
	}

	return nil
}

// UploadBackup uploads a backup to the backup provider
func (b *BackupProviderS3) UploadBackup(ctx context.Context, reader io.Reader) error {
	bucket := aws.String(b.config.BucketName)

	destination := defaultBackupName + b.suffix

	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debug("uploading object", "dest", destination)

	uploader := manager.NewUploader(b.c)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: bucket,
		Key:    aws.String(destination),
		Body:   reader,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetNextBackupName returns a name for the next backup archive that is going to be uploaded
func (b *BackupProviderS3) GetNextBackupName(_ context.Context) string {
	// name is constant because we use lifecycle rule to cleanup
	return b.config.BackupName
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderS3) ListBackups(ctx context.Context) (providers.BackupVersions, error) {
	it, err := b.c.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
		Bucket: aws.String(b.config.BucketName),
		Prefix: &b.config.ObjectPrefix,
	})
	if err != nil {
		return nil, err
	}

	return backupVersionsS3{
		objectAttrs: it.Versions,
	}, nil
}
