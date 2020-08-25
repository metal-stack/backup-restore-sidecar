package s3

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"

	"go.uber.org/zap"

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
	log    *zap.SugaredLogger
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
func New(log *zap.SugaredLogger, config *BackupProviderConfigS3) (*BackupProviderS3, error) {

	if config == nil {
		return nil, errors.New("s3 backup provider requires a provider config")
	}

	if config.ObjectsToKeep == 0 {
		config.ObjectsToKeep = constants.DefaultObjectsToKeep
	}
	if config.BackupName == "" {
		config.BackupName = defaultBackupName
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
	}, nil
}

// EnsureBackupBucket ensures a backup bucket at the backup provider
func (b *BackupProviderS3) EnsureBackupBucket() error {
	bucket := aws.String(b.config.BucketName)

	// create bucket
	cparams := &s3.CreateBucketInput{
		Bucket: bucket,
	}

	_, err := b.c.CreateBucket(cparams)
	if err != nil {
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
	_, err = b.c.PutBucketVersioning(versioning)
	if err != nil {
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
	_, err = b.c.PutBucketLifecycleConfiguration(lifecycle)
	if err != nil {
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
func (b *BackupProviderS3) CleanupBackups() error {
	// nothing to do here, done with lifecycle rules
	return nil
}

// DownloadBackup downloads the given backup version to the restoration folder
func (b *BackupProviderS3) DownloadBackup(version *providers.BackupVersion) error {
	bucket := aws.String(b.config.BucketName)

	downloadFileName := version.Name
	if strings.Contains(downloadFileName, "/") {
		downloadFileName = filepath.Base(downloadFileName)
	}
	backupFilePath := path.Join(constants.DownloadDir, downloadFileName)
	f, err := os.Create(backupFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	downloader := s3manager.NewDownloader(b.sess)

	_, err = downloader.Download(f,
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
func (b *BackupProviderS3) UploadBackup(sourcePath string) error {
	bucket := aws.String(b.config.BucketName)

	r, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer r.Close()

	destination := filepath.Base(sourcePath)
	if b.config.ObjectPrefix != "" {
		destination = b.config.ObjectPrefix + "/" + destination
	}

	b.log.Debugw("uploading object", "src", sourcePath, "dest", destination)

	uploader := s3manager.NewUploader(b.sess)
	_, err = uploader.Upload(&s3manager.UploadInput{
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
func (b *BackupProviderS3) GetNextBackupName() string {
	// name is constant because we use lifecycle rule to cleanup
	return b.config.BackupName
}

// ListBackups lists the available backups of the backup provider
func (b *BackupProviderS3) ListBackups() (providers.BackupVersions, error) {
	bucket := aws.String(b.config.BucketName)

	it, err := b.c.ListObjectVersions(&s3.ListObjectVersionsInput{
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
