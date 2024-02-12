package localfs

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/spf13/afero"
)

const (
	backupDest = constants.BackupDir + "/localfs"
	restoreSrc = constants.RestoreDir + "/localfs"
)

type LocalFS struct {
	datadir   string
	fileNames []string
	log       *slog.Logger
}

func New(log *slog.Logger, datadir string) *LocalFS {
	return &LocalFS{
		datadir: datadir,
		log:     log,
	}
}

// Check if Datadir contains the specified filenames
func (l *LocalFS) Check(ctx context.Context) (bool, error) {
	dirEntrys, err := os.ReadDir(l.datadir)

	if err != nil {
		return false, err
	}

	result := true

	for i := 0; i < len(l.fileNames) && result; i++ {
		found := false

		for j := 0; j < len(dirEntrys) && !found; j++ {
			curEntry, err := dirEntrys[j].Info()

			if err != nil {
				return false, err
			}

			found = l.fileNames[i] == curEntry.Name()
		}

		result = found
	}

	return result, nil
}

// put Datadir into constants.Backupdir directory
func (l *LocalFS) Backup(ctx context.Context) error {
	//ToDo: put Datadir into compressed archive

	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	if err := utils.Copy(afero.NewOsFs(), l.datadir, backupDest); err != nil {
		return fmt.Errorf("could not copy contents: %w", err)
	}

	l.log.Debug("Sucessfully took backup of localfs")
	return nil
}

func (l *LocalFS) Recover(ctx context.Context) error {
	//ToDo: decompress archive into Datadir
	if err := utils.RemoveContents(l.datadir); err != nil {
		return fmt.Errorf("Could not cleanup Datadir: %w", err)
	}

	if err := utils.Copy(afero.NewOsFs(), restoreSrc, l.datadir); err != nil {
		return fmt.Errorf("could not copy contents: %w", err)
	}

	l.log.Debug("Successfully restored localfs")
	return nil
}

func (l *LocalFS) Probe(ctx context.Context) error {
	//Nothing to do, not a real Database
	return nil
}

func (_ *LocalFS) Upgrade(ctx context.Context) error {
	// Nothing to do here
	return nil
}
