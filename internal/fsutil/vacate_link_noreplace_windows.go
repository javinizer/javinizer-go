//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// vacateLinkObjectOsFs is the Windows kernel leg of the symlink-shaped
// vacate: MoveFileEx WITHOUT MOVEFILE_REPLACE_EXISTING renames the reparse
// point OBJECT itself (never its target) and fails ERROR_ALREADY_EXISTS
// atomically on an occupied terminal — the same primitive the OsFs leg of
// PublishNoReplace uses for regular files, so the classes mirror it exactly.
func vacateLinkObjectOsFs(name, terminal string) error {
	srcPtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("symlink vacate MoveFileEx %s: %w", name, err)
	}
	dstPtr, err := windows.UTF16PtrFromString(terminal)
	if err != nil {
		return fmt.Errorf("symlink vacate MoveFileEx %s: %w", terminal, err)
	}
	err = windows.MoveFileEx(srcPtr, dstPtr, 0)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("symlink vacate MoveFileEx %s -> %s: %w", name, terminal, err)
	case errors.Is(err, os.ErrExist):
		return publishCollision(terminal)
	default:
		return fmt.Errorf("symlink vacate MoveFileEx %s -> %s: %w", name, terminal, err)
	}
}
