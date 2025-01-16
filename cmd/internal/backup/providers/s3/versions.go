package s3

import (
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/common"
)

// backupVersionsS3 contains the list of available backup versions
type backupVersionsS3 struct {
	objectAttrs []types.ObjectVersion
}

// Latest returns latest backup version
func (b backupVersionsS3) Latest() *providers.BackupVersion {
	return common.Latest(b.List())
}

// List return a list of all backup versions
func (b backupVersionsS3) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	for _, attr := range b.objectAttrs {
		result = append(result, &providers.BackupVersion{
			Name:    *attr.Key,
			Version: *attr.VersionId,
			Date:    *attr.LastModified,
		})
	}

	common.Sort(result)

	return result
}

// Get returns the backup entry of the given version
func (b backupVersionsS3) Get(version string) (*providers.BackupVersion, error) {
	return common.Get(b.List(), version)
}
