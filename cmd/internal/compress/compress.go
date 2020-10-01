package compress

import (
	"fmt"
	"path/filepath"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/constants"
	"github.com/mholt/archiver/v3"
)

type (
	// Compressor compress/decompress backup data before/after sending/receiving from storage
	Compressor struct {
		archiver  archiver.Archiver
		extension string
	}

	// Method defines possible compression methods
	Method uint
)

const (
	// TAR all files without compression, is suitable if content is already compressed
	TAR Method = iota
	// TARGZ compression
	TARGZ
	// TARLZ4 is much faster than GZIP with slightly bigger files
	TARLZ4
)

// New Returns a new Compressor with this method
func New(method string) (*Compressor, error) {
	var c *Compressor
	switch method {
	case "tar":
		c.archiver = archiver.NewTar()
		c.extension = ".tar"
	case "targz":
		c.archiver = archiver.NewTarGz()
		c.extension = ".tar.gz"
	case "tarlz4":
		c.archiver = archiver.NewTarLz4()
		c.extension = ".tar.lz4"
	default:
		return nil, fmt.Errorf("unsupported compression method: %s", method)
	}
	return c, nil
}

// Compress the given backupFile and returns the full filename with the extension
func (c *Compressor) Compress(backupFilePath string) (string, error) {
	filename := backupFilePath + c.extension
	return filename, c.archiver.Archive([]string{constants.BackupDir}, filename)
}

// Decompress the given backupFile
func (c *Compressor) Decompress(backupFilePath string) error {
	return archiver.Unarchive(backupFilePath, filepath.Dir(constants.RestoreDir))
}

// Extension returns the file extension of the configured compressor, depending on the method
func (c *Compressor) Extension() string {
	return c.extension
}
