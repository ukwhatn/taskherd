package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes to a temp file, fsyncs it, renames it over path and fsyncs the parent
// directory: durability of the rename itself requires that last fsync, not just the file's.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("一時ファイルを作れない: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("一時ファイルの権限を設定できない: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("一時ファイルに書けない: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("一時ファイルを fsync できない: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("一時ファイルを閉じられない: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename できない: %w", err)
	}
	return fsyncDir(dir)
}

func fsyncDir(dir string) error {
	fh, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%s を開けない: %w", dir, err)
	}
	defer func() {
		_ = fh.Close()
	}()

	if err := fh.Sync(); err != nil {
		return fmt.Errorf("%s を fsync できない: %w", dir, err)
	}
	return nil
}
