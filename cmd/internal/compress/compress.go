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
	// TAR all files without compression, is suitable if content is alread compressed like postgres
	TAR Method = iota
	// TARGZ compression
	TARGZ
	// TARLZ4 is much faster than GZIP with slightly bigger files
	TARLZ4
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
	case TARGZ:
		return filename, archiver.NewTarGz().Archive([]string{constants.BackupDir}, filename)
	case TARLZ4:
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

func MethodFrom(method string) (Method, error) {
	switch method {
	case "tar":
		return TAR, nil
	case "targz":
		return TARGZ, nil
	case "tarlz4":
		return TARLZ4, nil
	default:
		return TARGZ, fmt.Errorf("unknown method %s returning targz", method)
	}
}
