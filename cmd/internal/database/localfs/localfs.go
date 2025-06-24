package localfs

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/utils"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
)

type LocalFS struct {
	datadir string
	log     *slog.Logger
}

func New(log *slog.Logger, datadir string) *LocalFS {
	return &LocalFS{
		datadir: datadir,
		log:     log,
	}
}

// Check if Datadir is empty
func (l *LocalFS) Check(ctx context.Context) (bool, error) {
	empty, err := utils.IsEmpty(l.datadir)
	if err != nil {
		return false, err
	}
	if empty {
		l.log.Info("data directory is empty")
		return true, err
	}

	return false, nil
}

// put Datadir into constants.BackupDir directory
func (l *LocalFS) Backup(ctx context.Context) error {
	if err := os.RemoveAll(constants.BackupDir); err != nil {
		return fmt.Errorf("could not clean backup directory: %w", err)
	}

	if err := os.MkdirAll(constants.BackupDir, 0777); err != nil {
		return fmt.Errorf("could not create backup directory: %w", err)
	}

	if err := utils.CopyFS(constants.BackupDir, os.DirFS(l.datadir)); err != nil {
		return fmt.Errorf("could not copy contents: %w", err)
	}

	l.log.Debug("Successfully took backup of localfs")
	return nil
}

// get data from constants.RestoreDir
func (l *LocalFS) Recover(ctx context.Context) error {
	if err := utils.RemoveContents(l.datadir); err != nil {
		return fmt.Errorf("could not cleanup datadir: %w", err)
	}

	if err := utils.CopyFS(l.datadir, os.DirFS(constants.RestoreDir)); err != nil {
		return fmt.Errorf("could not copy contents: %w", err)
	}

	l.log.Debug("Successfully restored localfs")
	return nil
}

func (l *LocalFS) Probe(ctx context.Context) error {
	//Nothing to do, not a real Database
	return nil
}

func (*LocalFS) Upgrade(ctx context.Context) error {
	// Nothing to do here
	return nil
}
