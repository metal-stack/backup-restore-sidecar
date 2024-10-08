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
