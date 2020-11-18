package localfs

import "go.uber.org/zap"

type LocalFS struct {
	datadir string
	log     *zap.SugaredLogger
}

func New(log *zap.SugaredLogger, datadir string) *LocalFS {
	return &LocalFS{
		datadir: datadir,
		log:     log,
	}
}

func (l *LocalFS) Check() (bool, error) {
	//ToDo: check if Datadir empty -> true
	return true, nil
}

func (l *LocalFS) Backup() error {
	//ToDo: put Datadir into compressed archive
	return nil
}

func (l *LocalFS) Recover() error {
	//ToDo: decompress archive into Datadir
	return nil
}

func (l *LocalFS) Probe() error {
	//Nothing to do, not a real Database
	return nil
}
