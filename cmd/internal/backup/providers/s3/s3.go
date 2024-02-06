package s3

import (
	"context"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const (
	defaultBackupName = "db"
)

// BackupProviderS3 implements the backup provider interface for S3
type BackupProviderS3 struct {
	fs     afero.Fs
	log    *slog.Logger
	c      *s3.S3
	sess   *session.Session
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

	return nil
}

// New returns a S3 backup provider
func New(log *slog.Logger, config *BackupProviderConfigS3) (*BackupProviderS3, error) {
	if config == nil {
		return nil, errors.New("s3 backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.BackupName == "" {
		config.BackupName = defaultBackupName
	}
	if config.FS == nil {
		config.FS = afero.NewOsFs()
	}

	err := config.validate()
	if err != nil {
		return nil, err
	}
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, ""),
		Endpoint:         aws.String(config.Endpoint),
		Region:           aws.String(config.Region),
		S3ForcePathStyle: aws.Bool(true),
	}
	newSession, err := session.NewSession(s3Config)
	if err != nil {
		return nil, err
	}
	client := s3.New(newSession)
	if err != nil {
		return nil, err
	}

	return &BackupProviderS3{
		c:      client,
		sess:   newSession,
		config: config,
		log:    log,
		fs:     config.FS,
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderS3) EnsureBackupBucket(ctx context.Context) error {
	bucket := aws.String(b.config.BucketName)

	// create bucket
	cparams := &s3.CreateBucketInput{
		Bucket: bucket,
	}

	_, err := b.c.CreateBucketWithContext(ctx, cparams)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
			case s3.ErrCodeBucketAlreadyOwnedByYou:
			default:
				return err
			}
		} else {
			return err
		}
	}

	// enable versioning
	versioning := &s3.PutBucketVersioningInput{
		Bucket: bucket,
		VersioningConfiguration: &s3.VersioningConfiguration{
			Status: aws.String("Enabled"),
		},
	}
	_, err = b.c.PutBucketVersioningWithContext(ctx, versioning)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				return err
			}
		} else {
			return err
		}
	}

	// add lifecyle policy
	lifecycle := &s3.PutBucketLifecycleConfigurationInput{
		Bucket: bucket,
		LifecycleConfiguration: &s3.BucketLifecycleConfiguration{
			Rules: []*s3.LifecycleRule{
				{
					NoncurrentVersionExpiration: &s3.NoncurrentVersionExpiration{
						NoncurrentDays: &b.config.ObjectsToKeep,
					},
					Status: aws.String("Enabled"),
					ID:     aws.String("backup-restore-lifecycle"),
					Prefix: &b.config.ObjectPrefix,
				},
			},
		},
	}
	_, err = b.c.PutBucketLifecycleConfigurationWithContext(ctx, lifecycle)
	if err != nil {
		// FIXME check how to migrate to errors.As
		//nolint
		if aerr, ok := err.(awserr.Error); ok {
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
	bucket := aws.String(b.config.BucketName)

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

	downloader := s3manager.NewDownloader(b.sess)

	_, err = downloader.DownloadWithContext(
		ctx,
		f,
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
func (b *BackupProviderS3) UploadBackup(ctx context.Context, sourcePath string) error {
	bucket := aws.String(b.config.BucketName)

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

	uploader := s3manager.NewUploader(b.sess)
	_, err = uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: bucket,
		Key:    aws.String(destination),
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
	bucket := aws.String(b.config.BucketName)

	it, err := b.c.ListObjectVersionsWithContext(ctx, &s3.ListObjectVersionsInput{
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
