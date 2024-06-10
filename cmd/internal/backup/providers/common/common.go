package common

import (
	"slices"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

func Sort(versions []*providers.BackupVersion, oldestFirst bool) {
	slices.SortFunc(versions, func(a, b *providers.BackupVersion) int {
		if oldestFirst {
			return b.Date.Compare(a.Date)
		}
		return a.Date.Compare(b.Date)
	})
}
