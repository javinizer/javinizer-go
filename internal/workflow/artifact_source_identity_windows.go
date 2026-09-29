//go:build windows

package workflow

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// artifactSourceDevIno captures the collision-resistant file identity a
// stat-only Windows FileInfo cannot expose: Win32FileAttributeData carries no
// file key, and the volume/index pair Go's os.SameFile compares lives in an
// unexported sys struct. Identity capability is taken from the probed
// FileInfo — Sys() must carry the real OS attribute record, the same posture
// the POSIX leg takes on a masked Sys — and the admitted path is then pinned
// through the OS handle: GetFileInformationByHandle's
// VolumeSerialNumber+FileIndex is the NTFS object key, so a downloader
// swapping the admitted source for a different regular file with the size and
// modification time preserved is refused exactly like a POSIX inode swap
// (codex P1, the deferred-publication identity window). Gating on the
// FileInfo rather than the static fs type keeps the probe symmetric with
// POSIX: a wrapper filesystem that transports the underlying OS FileInfo
// unchanged keeps a working strong probe, while a lookup whose Sys leg is
// altered reports not-OK there too. The handle result is cross-checked
// against the probed record (attributes, size, last write): the returned key
// must name the same directory entry the lookup described, so a wrapper
// resolving path through another namespace degrades instead of pinning a
// foreign file's key. FILE_FLAG_OPEN_REPARSE_POINT keeps the capture bound
// to the named directory entry: a link swapped into the path inside the
// lstat→open window opens as the link object and its attributes report a
// reparse point, which deliberately degrades instead of capturing the link
// TARGET's key. In-memory filesystems (Sys()==nil) and handles whose server
// exposes no file key degrade to not-OK, keeping the size+modtime legs
// exactly like the POSIX in-memory posture.
func artifactSourceDevIno(_ afero.Fs, path string, info os.FileInfo) (device, inode uint64, ok bool) {
	stat, statOK := info.Sys().(*syscall.Win32FileAttributeData)
	if !statOK || stat == nil {
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
	if byHandle.FileAttributes != stat.FileAttributes ||
		byHandle.FileSizeHigh != stat.FileSizeHigh ||
		byHandle.FileSizeLow != stat.FileSizeLow ||
		byHandle.LastWriteTime.HighDateTime != stat.LastWriteTime.HighDateTime ||
		byHandle.LastWriteTime.LowDateTime != stat.LastWriteTime.LowDateTime {
		return 0, 0, false
	}
	return uint64(byHandle.VolumeSerialNumber),
		uint64(byHandle.FileIndexHigh)<<32 | uint64(byHandle.FileIndexLow), true
}
