package compress

import (
	"fmt"
	"path/filepath"

	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/mholt/archiver/v3"
)

type (
	// Compressor compress/decompress backup data before/after sending/receiving from storage
	Compressor struct {
		extension string
	}
)

// New Returns a new Compressor
func New(method string) (*Compressor, error) {
	c := &Compressor{}
	switch method {
	case "tar":
		c.extension = ".tar"
	case "targz":
		c.extension = ".tar.gz"
	case "tarlz4":
		c.extension = ".tar.lz4"
	default:
		return nil, fmt.Errorf("unsupported compression method: %s", method)
	}
	return c, nil
}

// Compress the given backupFile and returns the full filename with the extension
func (c *Compressor) Compress(backupFilePath string) (string, error) {
	filename := backupFilePath + c.extension
	return filename, archiver.Archive([]string{constants.BackupDir}, filename)
}

// Decompress the given backupFile
func (c *Compressor) Decompress(backupFilePath string) error {
	return archiver.Unarchive(backupFilePath, filepath.Dir(constants.RestoreDir))
}

// Extension returns the file extension of the configured compressor, depending on the methodn
func (c *Compressor) Extension() string {
	return c.extension
}
