package fsutil

import "github.com/spf13/afero"

// TryRemoveEmptyDir removes one directory if it can be read and is empty.
// It returns false if the path could not be read, is not empty, or removal
// fails. It neither walks parents nor removes descendants.
func TryRemoveEmptyDir(fs afero.Fs, path string) bool {
	entries, err := afero.ReadDir(fs, path)
	if err != nil || len(entries) != 0 {
		return false
	}
	return fs.Remove(path) == nil
}
