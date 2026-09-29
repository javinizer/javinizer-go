//go:build windows

package workflow

import (
	"os"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// artifactSourceDevIno captures the collision-resistant file identity a
// stat-only Windows FileInfo cannot expose: Win32FileAttributeData carries no
// file key, and the volume/index pair Go's os.SameFile compares lives in an
// unexported sys struct. The admitted path is therefore pinned through the OS
// handle — GetFileInformationByHandle's VolumeSerialNumber+FileIndex is the
// NTFS object key — so a downloader swapping the admitted source for a
// different regular file with the size and modification time preserved is
// refused exactly like a POSIX inode swap (codex P1, the deferred-publication
// identity window). FILE_FLAG_OPEN_REPARSE_POINT keeps the capture bound to
// the named directory entry: a link swapped into the path inside the
// lstat→open window opens as the link object and its attributes report a
// reparse point, which deliberately degrades instead of capturing the link
// TARGET's key. Virtual filesystems (memfs, identity-free wrappers) and
// handles whose server exposes no file key degrade to not-OK, keeping the
// size+modtime legs exactly like the POSIX in-memory posture.
func artifactSourceDevIno(fs afero.Fs, path string, _ os.FileInfo) (device, inode uint64, ok bool) {
	if _, isOS := fs.(*afero.OsFs); !isOS {
		return 0, 0, false
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	handle, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(handle)
	var byHandle windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &byHandle); err != nil {
		return 0, 0, false
	}
	if byHandle.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return 0, 0, false
	}
	return uint64(byHandle.VolumeSerialNumber),
		uint64(byHandle.FileIndexHigh)<<32 | uint64(byHandle.FileIndexLow), true
}
