package compress

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/mholt/archives"
	"github.com/spf13/afero"
)

type Compressor struct {
	fs         afero.Fs
	compressor *archives.CompressedArchive
	extension  string
}

type CompressorConfig struct {
	Method string
	FS     afero.Fs
}

// New Returns a new Compressor
func New(config *CompressorConfig) (*Compressor, error) {

	if config.FS == nil {
		config.FS = afero.NewOsFs()
	}

	var c *archives.CompressedArchive
	switch config.Method {
	case "tar":
		c = &archives.CompressedArchive{
			Archival:   archives.Tar{},
			Extraction: archives.Tar{},
		}
	case "targz":
		c = &archives.CompressedArchive{
			Compression: archives.Gz{},
			Extraction:  archives.Tar{},
			Archival:    archives.Tar{},
		}
	case "tarlz4":
		c = &archives.CompressedArchive{
			Compression: archives.Lz4{},
			Extraction:  archives.Tar{},
			Archival:    archives.Tar{},
		}
	default:
		return nil, fmt.Errorf("unsupported compression method: %s", config.Method)
	}
	return &Compressor{
		compressor: c,
		extension:  "." + config.Method,
		fs:         config.FS,
	}, nil
}

// Compress the given backupFile and returns the full filename with the extension
func (c *Compressor) Compress(ctx context.Context, outputWriter io.Writer, files []archives.FileInfo) error {
	err := c.compressor.Archive(ctx, outputWriter, files)
	if err != nil {
		return fmt.Errorf("error while compressing file in compressor: %w", err)
	}
	return nil
}

// Decompress the given backupFile
func (c *Compressor) Decompress(ctx context.Context, inputReader io.Reader, restoreDir string) error {
	err := c.compressor.Extract(ctx, inputReader, func(ctx context.Context, f archives.FileInfo) error {
		// open archive file
		file, err := f.Open()
		if err != nil {
			return err
		}
		defer file.Close()
		// create file in restore directory
		outputFile, err := c.fs.Create(restoreDir + "/" + f.Name())
		if err != nil {
			return err
		}
		defer outputFile.Close()
		// copy file content
		_, err = io.Copy(outputFile, file)
		if err != nil {
			return err
		}
		return nil
	})
	return err
}

func (c *Compressor) BuildFilesForCompression(inputPath string, name string) ([]archives.FileInfo, error) {
	files := []archives.FileInfo{}
	stat, err := c.fs.Stat(inputPath)
	if err != nil {
		return nil, err
	}

	files = append(files, archives.FileInfo{
		FileInfo:      stat,
		Header:        c.extension,
		NameInArchive: name,
		Open: func() (fs.File, error) {
			return c.fs.Open(inputPath)
		},
	})

	return files, nil
}

func (c *Compressor) Extension() string {
	return c.extension
}
