package compress

import (
	"fmt"
	"path/filepath"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/mholt/archiver/v3"
)

type (
	// Compressor is responsible to compress and decompress backups
	Compressor interface {
		// Compress the given backupFile
		Compress(backupFilePath string) error
		// Decompress the given backupFile
		Decompress(backupFilePath string) error
	}

	// BackupCompressor is the compressor instance
	BackupCompressor struct {
		method Method
	}

	// Method defines possible compression methods
	Method uint
)

const (
	// GZIP compression
	GZIP Method = iota
	// LZ4 is much faster than GZIP with slightly bigger files
	LZ4
)

// New Returns a new Compressor with this method
func New(method Method) *BackupCompressor {
	return &BackupCompressor{
		method: method,
	}
}

func (c *BackupCompressor) Compress(backupFilePath string) error {
	switch c.method {
	case GZIP:
		return archiver.NewTarGz().Archive([]string{constants.BackupDir}, backupFilePath+".tar.gz")
	case LZ4:
		return archiver.NewTarLz4().Archive([]string{constants.BackupDir}, backupFilePath+".tar.lz4")
	default:
		return fmt.Errorf("unknown compression method:%d", c.method)
	}
}

func (c *BackupCompressor) Decompress(backupFilePath string) error {
	return archiver.Unarchive(backupFilePath, filepath.Dir(constants.RestoreDir))
}
