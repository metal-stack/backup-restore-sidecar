package utils

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// IsEmpty returns whether a directory is empty or not
func IsEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// RemoveContents removes all files from a directory, but not the directory itself
func RemoveContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

// Copy copies a file from source to a destination
func Copy(fs afero.Fs, src, dst string) error {
	in, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := fs.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}
