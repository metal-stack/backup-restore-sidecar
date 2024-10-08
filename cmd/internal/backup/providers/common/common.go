package common

import (
	"fmt"
	"sort"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

// Sort the given list of backup versions
func Sort(versions []*providers.BackupVersion) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Date.After(versions[j].Date)
	})
}

// Latest returns latest backup version
func Latest(versions []*providers.BackupVersion) *providers.BackupVersion {
	Sort(versions)
	if len(versions) == 0 {
		return nil
	}
	return versions[0]
}

// Latest returns the backup version at given version
func Get(versions []*providers.BackupVersion, version string) (*providers.BackupVersion, error) {
	for _, backup := range versions {
		if version == backup.Version {
			return backup, nil
		}
	}
	return nil, fmt.Errorf("version %q not found", version)
}
