package utils

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/afero"
)

// IsEmpty returns whether a directory is empty or not
func IsEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = f.Close()
	}()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// IsRestoreDirty checks whether the `.restore_in_progress` file exists in the provided directory,
// which indicates that a restore process was started but not yet completed successfully.
func IsRestoreDirty(dir string) (bool, error) {
	restoreMarkerPath := filepath.Join(dir, ".restore_in_progress")
	_, err := os.Stat(restoreMarkerPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MarkRestoreInProgress creates a file named `.restore_in_progress` in the provided directory
// to indicate that a restore process is currently in progress.
// If the file already exists, it does nothing.
// If there is an error while creating the file, it returns an error.
func MarkRestoreInProgress(dir string, version string, date string) error {
	restoreMarkerPath := filepath.Join(dir, ".restore_in_progress")

	f, err := os.OpenFile(restoreMarkerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// file already exists
			return nil
		}
		return fmt.Errorf("unable to create restore marker file: %w", err)
	}
	defer f.Close()

	// write version and date to the marker file for better debugging
	_, err = f.WriteString(fmt.Sprintf("version: %s\ndate: %s\n", version, date))
	if err != nil {
		return fmt.Errorf("unable to write version info to restore marker file: %w", err)
	}

	return nil
}

// UnmarkRestoreInProgress removes the `.restore_in_progress` file from the provided directory
// to indicate that a restore process has completed.
// If the file does not exist, it does nothing.
// If there is an error while removing the file, it returns an error.
func UnmarkRestoreInProgress(dir string) error {
	restoreMarkerPath := filepath.Join(dir, ".restore_in_progress")
	if err := os.Remove(restoreMarkerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("unable to remove restore marker file: %w", err)
	}

	return nil
}

// RemoveContents removes all files from a directory, but not the directory itself
func RemoveContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = d.Close()
	}()
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
	defer func() {
		_ = in.Close()
	}()
	out, err := fs.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

func IsCommandPresent(command string) bool {
	p, err := exec.LookPath(command)
	if err != nil {
		return false
	}

	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return false
	}

	return true
}

// TODO: replace once go-1.23 is released
func CopyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, error error) error {
		targ := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			if err := os.MkdirAll(targ, 0777); err != nil {
				return err
			}
			return nil
		}
		r, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			_ = r.Close()
		}()
		info, err := r.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(targ, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666|info.Mode()&0777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			_ = w.Close()
			return fmt.Errorf("copying %s: %w", path, err)
		}
		if err := w.Close(); err != nil {
			return err
		}
		return nil
	})
}
