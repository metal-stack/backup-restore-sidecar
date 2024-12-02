package compress

import (
	"context"
	"fmt"
	"io"

	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/mholt/archives"
)

type Compressor struct {
	compressor *archives.CompressedArchive
	extension  string
}

// New Returns a new Compressor
func New(method string) (*Compressor, error) {
	c := &archives.CompressedArchive{}
	switch method {
	case "tar":
		c = &archives.CompressedArchive{
			Archival: archives.Tar{},
		}
	case "targz":
		c = &archives.CompressedArchive{
			Compression: archives.Gz{},
			Archival:    archives.Tar{},
		}
	case "tarlz4":
		c = &archives.CompressedArchive{
			Compression: archives.Lz4{},
			Archival:    archives.Tar{},
		}
	default:
		return nil, fmt.Errorf("unsupported compression method: %s", method)
	}
	return &Compressor{compressor: c, extension: "." + method}, nil
}

// Compress the given backupFile and returns the full filename with the extension
func (c *Compressor) Compress(ctx context.Context, backupFilePath string, outputWriter io.Writer) error {
	files, err := archives.FilesFromDisk(ctx, &archives.FromDiskOptions{}, map[string]string{
		constants.BackupDir: backupFilePath + c.Extension(),
	})

	if err != nil {
		return fmt.Errorf("error while reading file in compressor: %w", err)
	}
	err = c.compressor.Archive(ctx, outputWriter, files)
	if err != nil {
		fmt.Printf("error while compressing file in compressor: %v", err)
	}
	return nil
}

// Decompress the given backupFile
func (c *Compressor) Decompress(ctx context.Context, inputReader io.Reader) error {
	err := c.compressor.Extract(ctx, inputReader, func(ctx context.Context, f archives.FileInfo) error {
		// do something with the file here; or, if you only want a specific file or directory,
		// just return until you come across the desired f.NameInArchive value(s)
		return nil
	})
	return err
}

func (c *Compressor) Extension() string {
	return c.extension
}
