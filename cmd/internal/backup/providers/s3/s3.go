package s3

import (
	"context"
	"io"
	"log/slog"
	"strconv"

	"errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/compress"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/encryption"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
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
	ProviderConstant = "db"
)

// BackupProviderS3 implements the backup provider interface for S3
type BackupProviderS3 struct {
	fs         afero.Fs
	log        *slog.Logger
	c          *s3.S3
	sess       *session.Session
	config     *BackupProviderConfigS3
	encrypter  *encryption.Encrypter
	compressor *compress.Compressor
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
	Encrypter     *encryption.Encrypter
	Compressor    *compress.Compressor
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

	return &BackupProviderS3{
		c:          client,
		sess:       newSession,
		config:     config,
		log:        log,
		fs:         config.FS,
		encrypter:  config.Encrypter,
		compressor: config.Compressor,
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

	// add lifecycle policy
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

// DownloadBackup downloads the given backup version to the specified folder
func (b *BackupProviderS3) DownloadBackup(ctx context.Context, version *providers.BackupVersion, writer io.Writer) error {
	gen, err := strconv.ParseInt(version.Version, 10, 64)
	if err != nil {
		return err
	}

	bucket := aws.String(b.config.BucketName)

	downloader := s3manager.NewDownloader(b.sess)
	// we need to download the backup sequentially since we fake the download with a io.Writer instead of io.WriterAt
	downloader.Concurrency = 1

	b.log.Info("downloading", "object", version.Name, "get", gen)

	streamWriter := utils.NewSequentialWriterAt(writer)
	_, err = downloader.DownloadWithContext(
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

	destination := ProviderConstant
	if b.compressor != nil {
		destination += b.compressor.Extension()
	}
	if b.encrypter != nil {
		destination += b.encrypter.Extension()
	}

	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debug("uploading object", "dest", destination)

	uploader := s3manager.NewUploader(b.sess)
	_, err := uploader.UploadWithContext(ctx, &s3manager.UploadInput{
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
	bucket := aws.String(b.config.BucketName)

	it, err := b.c.ListObjectVersionsWithContext(ctx, &s3.ListObjectVersionsInput{
		Bucket: bucket,
		Prefix: &b.config.ObjectPrefix,
	})
	if err != nil {
		return nil, err
	}

	return backupVersionsS3{
		objectAttrs: it.Versions,
	}, nil
}
