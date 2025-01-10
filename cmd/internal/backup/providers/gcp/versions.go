package gcp

import (
	"strconv"

	"cloud.google.com/go/storage"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/common"
)

type backupVersionsGCP struct {
	objectAttrs []*storage.ObjectAttrs
}

func (b backupVersionsGCP) Latest() *providers.BackupVersion {
	return common.Latest(b.List())
}

func (b backupVersionsGCP) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	tmp := make(map[int64]bool, len(result))
	for _, attr := range b.objectAttrs {
		ok := tmp[attr.Generation]
		if !ok {
			tmp[attr.Generation] = true
			result = append(result, &providers.BackupVersion{
				Name:    attr.Name,
				Version: strconv.FormatInt(attr.Generation, 10),
				Date:    attr.Updated,
			})
		}
	}

	common.Sort(result)

	return result
}

func (b backupVersionsGCP) Get(version string) (*providers.BackupVersion, error) {
	return common.Get(b.List(), version)
}
