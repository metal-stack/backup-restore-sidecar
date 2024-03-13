package s3

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"

	awsv1 "github.com/aws/aws-sdk-go/aws"
	awserrv1 "github.com/aws/aws-sdk-go/aws/awserr"
	credentialsv1 "github.com/aws/aws-sdk-go/aws/credentials"
	sessionv1 "github.com/aws/aws-sdk-go/aws/session"
	s3v1 "github.com/aws/aws-sdk-go/service/s3"
	s3managerv1 "github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const (
	defaultBackupName = "db"
)

// BackupProviderS3 implements the backup provider interface for S3
type BackupProviderS3 struct {
	fs     afero.Fs
	log    *slog.Logger
	c      *s3v1.S3
	sess   *sessionv1.Session
	config *BackupProviderConfigS3
}

// BackupProviderConfigS3 provides configuration for the BackupProviderS3
type BackupProviderConfigS3 struct {
	BucketName    string
	Endpoint      string
	Region        string
	AccessKey     string
	SecretKey     string
	BackupName    string
	ObjectPrefix  string
	ObjectsToKeep int64
	FS            afero.Fs
}

func (c *BackupProviderConfigS3) validate() error {
	if c.BucketName == "" {
		return errors.New("s3v1 bucket name must not be empty")
	}
	if c.Endpoint == "" {
		return errors.New("s3v1 endpoint must not be empty")
	}
	if c.AccessKey == "" {
		return errors.New("s3v1 accesskey must not be empty")
	}
	if c.SecretKey == "" {
		return errors.New("s3v1 secretkey must not be empty")
	}

	return nil
}

// New returns a S3 backup provider
func New(log *slog.Logger, cfg *BackupProviderConfigS3) (*BackupProviderS3, error) {
	if cfg == nil {
		return nil, errors.New("s3v1 backup provider requires a provider config")
	}

	if cfg.ObjectsToKeep == 0 {
		cfg.ObjectsToKeep = constants.DefaultObjectsToKeep
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
	s3Config := &awsv1.Config{
		Credentials:      credentialsv1.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, ""),
		Endpoint:         awsv1.String(cfg.Endpoint),
		Region:           awsv1.String(cfg.Region),
		S3ForcePathStyle: awsv1.Bool(true),
	}
	newSession, err := sessionv1.NewSession(s3Config)
	if err != nil {
		return nil, err
	}
	client := s3v1.New(newSession)
	if err != nil {
		return nil, err
	}

	return &BackupProviderS3{
		c:      client,
		sess:   newSession,
		config: cfg,
		log:    log,
		fs:     cfg.FS,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderS3) EnsureBackupBucket(ctx context.Context) error {
	bucket := awsv1.String(b.config.BucketName)

	// create bucket
	cparams := &s3v1.CreateBucketInput{
		Bucket: bucket,
	}

	_, err := b.c.CreateBucketWithContext(ctx, cparams)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserrv1.Error); ok {
			switch aerr.Code() {
			case s3v1.ErrCodeBucketAlreadyExists:
			case s3v1.ErrCodeBucketAlreadyOwnedByYou:
			default:
				return err
			}
		} else {
			return err
		}
	}

	// enable versioning
	versioning := &s3v1.PutBucketVersioningInput{
		Bucket: bucket,
		VersioningConfiguration: &s3v1.VersioningConfiguration{
			Status: awsv1.String("Enabled"),
		},
	}
	_, err = b.c.PutBucketVersioningWithContext(ctx, versioning)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserrv1.Error); ok {
			switch aerr.Code() {
			default:
				return err
			}
		} else {
			return err
		}
	}

	// add lifecyle policy
	lifecycle := &s3v1.PutBucketLifecycleConfigurationInput{
		Bucket: bucket,
		LifecycleConfiguration: &s3v1.BucketLifecycleConfiguration{
			Rules: []*s3v1.LifecycleRule{
				{
					NoncurrentVersionExpiration: &s3v1.NoncurrentVersionExpiration{
						NoncurrentDays: &b.config.ObjectsToKeep,
					},
					Status: awsv1.String("Enabled"),
					ID:     awsv1.String("backup-restore-lifecycle"),
					Prefix: &b.config.ObjectPrefix,
				},
			},
		},
	}
	_, err = b.c.PutBucketLifecycleConfigurationWithContext(ctx, lifecycle)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserrv1.Error); ok {
			switch aerr.Code() {
			default:
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// CleanupBackups cleans up backups according to the given backup cleanup policy at the backup provider
func (b *BackupProviderS3) CleanupBackups(_ context.Context) error {
	// nothing to do here, done with lifecycle rules
	return nil
}

// DownloadBackup downloads the given backup version to the restoration folder
func (b *BackupProviderS3) DownloadBackup(ctx context.Context, version *providers.BackupVersion) error {
	bucket := awsv1.String(b.config.BucketName)

	downloadFileName := version.Name
	if strings.Contains(downloadFileName, "/") {
		downloadFileName = filepath.Base(downloadFileName)
	}

	backupFilePath := path.Join(constants.DownloadDir, downloadFileName)

	f, err := b.fs.Create(backupFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	downloader := s3managerv1.NewDownloader(b.sess)

	_, err = downloader.DownloadWithContext(
		ctx,
		f,
		&s3v1.GetObjectInput{
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
func (b *BackupProviderS3) UploadBackup(ctx context.Context, sourcePath string) error {
	bucket := awsv1.String(b.config.BucketName)

	r, err := b.fs.Open(sourcePath)
	if err != nil {
		return err
	}
	defer r.Close()

	destination := filepath.Base(sourcePath)
	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debug("uploading object", "src", sourcePath, "dest", destination)

	uploader := s3managerv1.NewUploader(b.sess)
	_, err = uploader.UploadWithContext(ctx, &s3managerv1.UploadInput{
		Bucket: bucket,
		Key:    awsv1.String(destination),
		Body:   r,
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
	bucket := awsv1.String(b.config.BucketName)

	it, err := b.c.ListObjectVersionsWithContext(ctx, &s3v1.ListObjectVersionsInput{
		Bucket: bucket,
		Prefix: &b.config.ObjectPrefix,
	})
	if err != nil {
		return nil, err
	}

	return BackupVersionsS3{
		objectAttrs: it.Versions,
	}, nil
}
