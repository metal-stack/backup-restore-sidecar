package local

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
)

type backupVersionsLocal struct {
	files []os.FileInfo
}

func (b backupVersionsLocal) Latest() *providers.BackupVersion {
	result := b.List()
	if len(result) == 0 {
		return nil
	}
	b.Sort(result, false)
	return result[0]
}

func (b backupVersionsLocal) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	for _, file := range b.files {
		result = append(result, &providers.BackupVersion{
			Name: file.Name(),
			Date: file.ModTime(),
		})
	}

	b.Sort(result, false)

	for i, backup := range result {
		backup.Version = strconv.FormatInt(int64(i), 10)
	}

	return result
}

func (b backupVersionsLocal) Sort(versions []*providers.BackupVersion, oldestFirst bool) {
	sort.Slice(versions, func(i, j int) bool {
		if oldestFirst {
			return versions[i].Date.Before(versions[j].Date)
		}
		return versions[i].Date.After(versions[j].Date)
	})
}

func (b backupVersionsLocal) Get(version string) (*providers.BackupVersion, error) {
	for _, backup := range b.List() {
		if version == backup.Version {
			return backup, nil
		}
	}
	return nil, fmt.Errorf("version %q not found", version)
}
