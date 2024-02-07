package localfs

import (
	"context"
	"log/slog"
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

func (l *LocalFS) Check(ctx context.Context) (bool, error) {
	//ToDo: check if Datadir empty -> true
	return true, nil
}

func (l *LocalFS) Backup(ctx context.Context) error {
	//ToDo: put Datadir into compressed archive
	return nil
}

func (l *LocalFS) Recover(ctx context.Context) error {
	//ToDo: decompress archive into Datadir
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
