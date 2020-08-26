package s3

import (
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

// BackupVersionsS3 contains the list of available backup versions
type BackupVersionsS3 struct {
	objectAttrs []*s3.ObjectVersion
}

// Latest returns latest backup version
func (b BackupVersionsS3) Latest() *providers.BackupVersion {
	result := b.List()
	if len(result) == 0 {
		return nil
	}
	b.Sort(result, false)
	return result[0]
}

// List return a list of all backup versions
func (b BackupVersionsS3) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	tmp := make(map[int64]bool)
	for _, attr := range b.objectAttrs {
		ok := tmp[attr.LastModified.Unix()]
		if !ok {
			tmp[attr.LastModified.Unix()] = true
			result = append(result, &providers.BackupVersion{
				Name:    *attr.Key,
				Version: *attr.VersionId,
				Date:    *attr.LastModified,
			})
		}
	}

	return result
}

// Sort returns the backup versions sorted by date
func (b BackupVersionsS3) Sort(versions []*providers.BackupVersion, asc bool) {
	sort.Slice(versions, func(i, j int) bool {
		if asc {
			return versions[i].Date.Before(versions[j].Date)
		}
		return versions[i].Date.After(versions[j].Date)
	})
}

// Get returns the backup entry of the given version
func (b BackupVersionsS3) Get(version string) (*providers.BackupVersion, error) {
	for _, backup := range b.List() {
		if version == backup.Version {
			return backup, nil
		}
	}
	return nil, fmt.Errorf("version %q not found", version)
}
