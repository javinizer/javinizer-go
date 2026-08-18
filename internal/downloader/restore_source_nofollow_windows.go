//go:build windows

package downloader

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// Windows has no O_NOFOLLOW flag, and os.OpenFile translates unknown bits
// away, so the constant stays inert. The real protection is
// openRestoreSourceWindows installed into restoreOpenReplacementSource in
// init.
const restoreSourceNoFollow = 0

// init installs the reparse-point restore open as the package-wide source
// opener before any rollback can run.
func init() {
	restoreOpenReplacementSource = openRestoreSourceWindows
}

// openRestoreSourceWindows opens a rollback backup WITHOUT following a
// final-component symlink: FILE_FLAG_OPEN_REPARSE_POINT makes CreateFile
// open the link object itself instead of its target, and the by-handle
// metadata then exposes FILE_ATTRIBUTE_REPARSE_POINT. A link swapped in
// between the Lstat gate and this open is therefore detected on the very
// handle that would be read, and refused with the same error class the POSIX
// O_NOFOLLOW open maps to at the call site. When no reparse point was
// opened, the reparse-opened descriptor itself is returned so the read is
// pinned to the object that passed every check (no second name lookup).
func openRestoreSourceWindows(fsys afero.Fs, backup string) (afero.File, error) {
	// Only OsFs paths name kernel objects; anything else (MemMapFs in tests)
	// keeps the plain-open fallback guided by the caller's Lstat gate.
	if _, ok := fsys.(*afero.OsFs); !ok {
		return fsys.OpenFile(backup, os.O_RDONLY|restoreSourceNoFollow, 0)
	}

	namep, err := windows.UTF16PtrFromString(restoreSourceLongPath(backup))
	if err != nil {
		return nil, err
	}
	// Match os.Open's share mode so a concurrent sweeper rename/delete is not
	// blocked by this read handle on Windows.
	handle, err := windows.CreateFile(
		namep,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, refuseRestoreSource(backup, "backup became a symlink before open")
	}
	// The reparse-opened descriptor is a regular non-link object: wrap THAT
	// handle so the read cannot drift to a swapped-in target.
	return os.NewFile(uintptr(handle), backup), nil
}

// restoreSourceLongPath mirrors os.Open's long-path handling: paths beyond
// classic MAX_PATH must be presented in extended form (\\?\-prefixed,
// absolute) or CreateFile rejects them. UNC shares use the \\?\UNC\ form.
func restoreSourceLongPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if len(abs) < windows.MAX_PATH {
		return abs
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
