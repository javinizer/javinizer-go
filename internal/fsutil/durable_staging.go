package fsutil

import (
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// SyncStagedMetadata prepares an owned native staging handle for a durable
// journal seal. Every metadata write is handle-scoped; unsupported platforms
// refuse rather than applying metadata through a possibly substituted name.
func SyncStagedMetadata(fs afero.Fs, file afero.File, source os.FileInfo) error {
	native, ok := osStagingHandle(fs, file)
	if !ok {
		return fmt.Errorf("durable staging requires a native file handle")
	}
	RestoreStagingOwnership(fs, file, source)
	// Ownership restoration can change permission bits. Re-prove the mode
	// afterwards, and avoid a redundant chmod on ACL-managed destinations.
	if mode := source.Mode().Perm(); !nativeStagingModeMatches(native, mode) {
		if err := applyStagingModeTolerant(native.Name(), func() error { return stagedHandleChmod(native, mode) }); err != nil {
			return err
		}
	}
	if err := stagedHandleChtimes(native.Fd(), source.ModTime(), source.ModTime()); err != nil {
		return err
	}
	return file.Sync()
}
