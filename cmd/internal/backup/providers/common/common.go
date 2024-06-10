package common

import (
	"fmt"
	"slices"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

// Sort the given list of backup versions
func Sort(versions []*providers.BackupVersion, oldestFirst bool) {
	slices.SortFunc(versions, func(a, b *providers.BackupVersion) int {
		if oldestFirst {
			return b.Date.Compare(a.Date)
		}
		return a.Date.Compare(b.Date)
	})
}

// Latest returns latest backup version
func Latest(versions []*providers.BackupVersion) *providers.BackupVersion {
	Sort(versions, true)
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
