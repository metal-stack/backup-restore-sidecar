package common

import (
	"reflect"
	"testing"
	"time"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers"
	"github.com/stretchr/testify/require"
)

func TestSort(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		versions       []*providers.BackupVersion
		wantedVersions []*providers.BackupVersion
	}{
		{
			name: "mixed",
			versions: []*providers.BackupVersion{
				{Name: "2.tgz", Date: now.Add(2 * time.Hour)},
				{Name: "1.tgz", Date: now.Add(1 * time.Hour)},
				{Name: "5.tgz", Date: now.Add(5 * time.Hour)},
				{Name: "0.tgz", Date: now},
				{Name: "3.tgz", Date: now.Add(3 * time.Hour)},
			},
			wantedVersions: []*providers.BackupVersion{
				{Name: "0.tgz", Date: now},
				{Name: "1.tgz", Date: now.Add(1 * time.Hour)},
				{Name: "2.tgz", Date: now.Add(2 * time.Hour)},
				{Name: "3.tgz", Date: now.Add(3 * time.Hour)},
				{Name: "5.tgz", Date: now.Add(5 * time.Hour)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Sort(tt.versions)
			require.ElementsMatch(t, tt.versions, tt.wantedVersions)
		})
	}
}

func TestLatest(t *testing.T) {
	now := time.Now()
	newestBackup := &providers.BackupVersion{Name: "5.tgz", Date: now.Add(5 * time.Hour)}
	tests := []struct {
		name     string
		versions []*providers.BackupVersion
		want     *providers.BackupVersion
	}{
		{
			versions: []*providers.BackupVersion{
				{Name: "2.tgz", Date: now.Add(2 * time.Hour)},
				{Name: "0.tgz", Date: now},
				{Name: "1.tgz", Date: now.Add(1 * time.Hour)},
				newestBackup,
				{Name: "3.tgz", Date: now.Add(3 * time.Hour)},
			},
			want: newestBackup,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Latest(tt.versions); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Latest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetermineBackupFilePath(t *testing.T) {
	outPath1 := ""
	outPath2 := "/backup/test"
	downloadDir := "/backup/restore"
	fileName := "0.tar.aes"

	wantBackupFilePath1 := "/backup/restore/0.tar.aes"
	wantBackupFilePath2 := "/backup/test/0.tar.aes"

	t.Run("empty-outpath", func(t *testing.T) {
		backupFilePath := DeterminBackupFilePath(outPath1, downloadDir, fileName)
		if backupFilePath != wantBackupFilePath1 {
			t.Errorf("DetermineBackupFilePath() = %v, want %v", backupFilePath, wantBackupFilePath1)
		}
	})

	t.Run("filled-outpath", func(t *testing.T) {
		backupFilePath := DeterminBackupFilePath(outPath2, downloadDir, fileName)
		if backupFilePath != wantBackupFilePath2 {
			t.Errorf("DetermineBackupFilePath() = %v, want %v", backupFilePath, wantBackupFilePath2)
		}
	})
}
