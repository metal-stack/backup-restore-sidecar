package gcp

import (
	"fmt"
	"sort"
	"strconv"

	"cloud.google.com/go/storage"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

type BackupVersionsGCP struct {
	objectAttrs []*storage.ObjectAttrs
}

func (b BackupVersionsGCP) Latest() *providers.BackupVersion {
	result := b.List()
	if len(result) == 0 {
		return nil
	}
	return result[0]
}

func (b BackupVersionsGCP) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	tmp := make(map[int64]bool)
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

	b.Sort(result, false)

	return result
}

func (b BackupVersionsGCP) Sort(versions []*providers.BackupVersion, asc bool) {
	sort.Slice(versions, func(i, j int) bool {
		if asc {
			return versions[i].Date.Before(versions[j].Date)
		}
		return versions[i].Date.After(versions[j].Date)
	})
}

func (b BackupVersionsGCP) Get(version string) (*providers.BackupVersion, error) {
	for _, backup := range b.List() {
		if version == backup.Version {
			return backup, nil
		}
	}
	return nil, fmt.Errorf("version %q not found", version)
}
