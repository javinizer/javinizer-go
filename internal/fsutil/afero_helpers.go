package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
)

// CanonicalizePath resolves a path to its absolute, cleaned, and normalized form.
// It combines filepath.Abs, filepath.Clean, and NormalizePath into a
// single operation used by revert_log and reverter for cross-platform path handling.
func CanonicalizePath(p string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	return NormalizePath(abs), nil
}

// AferoRemoveAll removes path and any children using the provided filesystem.
func AferoRemoveAll(fs afero.Fs, path string, admittedDirectory ...[2]uint64) error {
	if len(admittedDirectory) > 0 {
		identity := admittedDirectory[0]
		if err := removeDirectoryContentsBound(fs, path, identity[0], identity[1]); err != nil {
			return err
		}
		current, err := asideLstat(fs, path)
		if err != nil {
			return err
		}
		dev, ino, ok := BoundObjectIdentity(fs, path, current)
		if !ok || dev != identity[0] || ino != identity[1] || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory namespace %s changed after bound removal: %w", path, ErrTakeAsideForeign)
		}
		return fs.Remove(path)
	}
	info, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fs.Remove(path)
	}

	dirs := make([]string, 0)
	walkErr := afero.Walk(fs, path, func(current string, walkInfo os.FileInfo, innerErr error) error {
		if innerErr != nil {
			if os.IsNotExist(innerErr) {
				return nil
			}
			return innerErr
		}
		if walkInfo.IsDir() {
			dirs = append(dirs, current)
			return nil
		}
		return fs.Remove(current)
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return nil
		}
		return walkErr
	}

	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := fs.Remove(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
