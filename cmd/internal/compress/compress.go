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
		// Compress the given backupFile and returns the full filename with the extension
		Compress(backupFilePath string) (string, error)
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

func (c *BackupCompressor) Compress(backupFilePath string) (string, error) {
	filename := backupFilePath + c.method.Extension()
	switch c.method {
	case GZIP:
		return filename, archiver.NewTarGz().Archive([]string{constants.BackupDir}, filename)
	case LZ4:
		return filename, archiver.NewTarLz4().Archive([]string{constants.BackupDir}, filename)
	default:
		return filename, fmt.Errorf("unknown compression method:%d", c.method)
	}
}

func (c *BackupCompressor) Decompress(backupFilePath string) error {
	return archiver.Unarchive(backupFilePath, filepath.Dir(constants.RestoreDir))
}

func (c *BackupCompressor) Extension() string {
	return c.method.Extension()
}

func (m Method) Extension() string {
	switch m {
	case GZIP:
		return ".tar.gz"
	case LZ4:
		return ".tar.lz4"
	default:
		return ".tar.gz"
	}
}
