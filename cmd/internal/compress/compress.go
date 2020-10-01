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
		method Method
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
func New(method Method) *Compressor {
	return &Compressor{
		method: method,
	}
}

// Compress the given backupFile and returns the full filename with the extension
func (c *Compressor) Compress(backupFilePath string) (string, error) {
	filename := backupFilePath + c.method.Extension()
	var arch archiver.Archiver
	switch c.method {
	case TAR:
		arch = archiver.NewTar()
	case TARGZ:
		arch = archiver.NewTarGz()
	case TARLZ4:
		arch = archiver.NewTarLz4()
	default:
		return filename, fmt.Errorf("unknown compression method:%d", c.method)
	}
	return filename, arch.Archive([]string{constants.BackupDir}, filename)
}

// Decompress the given backupFile
func (c *Compressor) Decompress(backupFilePath string) error {
	return archiver.Unarchive(backupFilePath, filepath.Dir(constants.RestoreDir))
}

// Extension returns the file extension of the configured compressor, depending on the method
func (c *Compressor) Extension() string {
	return c.method.Extension()
}

// Extension returns the file extension of the configured method
func (m Method) Extension() string {
	switch m {
	case TAR:
		return ".tar"
	case TARGZ:
		return ".tar.gz"
	case TARLZ4:
		return ".tar.lz4"
	default:
		return ".tar.gz"
	}
}

// MethodFrom will choose the Method from a string representation
func MethodFrom(method string) (Method, error) {
	switch method {
	case "tar":
		return TAR, nil
	case "targz":
		return TARGZ, nil
	case "tarlz4":
		return TARLZ4, nil
	default:
		return TARGZ, fmt.Errorf("unknown method %q, returning targz", method)
	}
}
