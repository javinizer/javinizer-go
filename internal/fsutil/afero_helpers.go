package fsutil

import (
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
func AferoRemoveAll(fs afero.Fs, path string) error {
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
