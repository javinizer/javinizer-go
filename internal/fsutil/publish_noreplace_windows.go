//go:build windows

package fsutil

import (
	"errors"
	"os"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// PublishNoReplace atomically publishes staged onto dst WITHOUT replacing an
// occupied destination: MoveFileExW deliberately WITHOUT
// MOVEFILE_REPLACE_EXISTING (the mirror of replacefile_windows.go's
// ReplaceFile) fails ERROR_ALREADY_EXISTS when dst is occupied, mapped into
// ErrPublishCollision. Virtual filesystems take the shared
// classify-then-rename leg, whose rename-refusal mapping covers Windows
// MoveFileW-style renames.
//
// POSTER-WRITE-HARDENING wave-15 (codex P2): parity with the POSIX legs —
// the downloader's create path and the history backup re-arm must never
// rename over a foreign writer's mid-window bytes.
func PublishNoReplace(fs afero.Fs, src, dst string) error {
	if _, ok := fs.(*afero.OsFs); ok {
		srcPtr, err := windows.UTF16PtrFromString(src)
		if err != nil {
			return err
		}
		dstPtr, err := windows.UTF16PtrFromString(dst)
		if err != nil {
			return err
		}
		err = windows.MoveFileEx(srcPtr, dstPtr, 0)
		if err == nil {
			return nil
		}
		if errors.Is(err, os.ErrExist) {
			return publishCollision(dst)
		}
		return err
	}
	return publishNoReplaceVirtual(fs, src, dst)
}
