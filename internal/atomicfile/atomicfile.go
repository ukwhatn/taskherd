// Package atomicfile replaces a file's contents in one step, or not at all.
//
// Every file taskherd owns — tasks.json, cache.json, the update check's record — is read by other
// processes while it is being written, so none of them may ever be observed half-written. A reader
// that catches a torn file cannot tell it apart from a corrupt one.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write puts data at path, atomically.
//
// It writes to a temporary file in the same directory, fsyncs it, renames it over path and then
// fsyncs the parent directory: durability of the rename itself requires that last fsync, not just
// the file's. The temporary file shares a directory with the target so that the rename stays
// within one filesystem, which is what makes it atomic.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cannot create the temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("cannot set the mode on the temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("cannot write the temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("cannot fsync the temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close the temporary file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot rename into place: %w", err)
	}
	return fsyncDir(dir)
}

func fsyncDir(dir string) error {
	fh, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", dir, err)
	}
	defer func() {
		_ = fh.Close()
	}()

	if err := fh.Sync(); err != nil {
		return fmt.Errorf("cannot fsync %s: %w", dir, err)
	}
	return nil
}
