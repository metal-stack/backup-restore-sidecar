package local

import (
	"os"
	"strconv"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/common"
)

type backupVersionsLocal struct {
	files []os.FileInfo
}

func (b backupVersionsLocal) Latest() *providers.BackupVersion {
	return common.Latest(b.List())
}

func (b backupVersionsLocal) List() []*providers.BackupVersion {
	var result []*providers.BackupVersion

	for _, file := range b.files {
		result = append(result, &providers.BackupVersion{
			Name: file.Name(),
			Date: file.ModTime(),
		})
	}

	common.Sort(result, false)

	for i, backup := range result {
		backup.Version = strconv.FormatInt(int64(i), 10)
	}

	return result
}

func (b backupVersionsLocal) Get(version string) (*providers.BackupVersion, error) {
	return common.Get(b, version)
}
