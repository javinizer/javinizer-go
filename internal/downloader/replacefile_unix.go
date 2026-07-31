//go:build !windows

package downloader

import "github.com/spf13/afero"

func replaceFile(fs afero.Fs, src, dst string) error {
	return fs.Rename(src, dst)
}
