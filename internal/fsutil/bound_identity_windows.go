//go:build windows

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// boundObjectIdentity keeps the take-aside binding's wave-29 posture on
// Windows: a stat-only FileInfo exposes no comparable kernel key there
// (Win32FileAttributeData carries no volume/index pair, and the vol+idx legs
// os.SameFile compares live in an unexported sys struct), so asideSameObject
// keeps the no-follow Lstat + size + mtime legs. Callers pinning an
// ADMISSION identity use BoundObjectIdentity below instead.
func boundObjectIdentity(os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}

// BoundObjectIdentity captures the collision-resistant file identity a
// stat-only Windows FileInfo cannot expose: the volume-serial + file-index
// handle identity the workflow's admission gating introduced (moved here so
// every caller-side identity proof — the fenced publication's deferred-source
// binding, the organizer's verified-dispatch proofs, and the verified
// composites' own test proofs — rides ONE build-tagged route instead of each
// growing its own GetFileInformationByHandle copy). Capture is EAGER at
// the call instant: os.SameFile is not a substitute, because a path-captured
// FileInfo records the path and re-opens it AT COMPARE TIME (the lazy
// file-id load behind os.SameFile): by the verified move's claim re-proof
// the source name is
// already vacated (SameFile fails closed and refuses the admitted object's
// own happy path), and after a pre-execute swap the same lazy pin binds the
// REPLACEMENT (the refusal the proof exists to deliver is swallowed).
//
// Capability is gated on the probed FileInfo — Sys() must carry the real OS
// attribute record, the same posture the POSIX leg takes on a masked Sys —
// and the named entry is pinned through the OS handle: a downloader swapping
// the admitted source for a different regular file with size and modification
// time preserved is refused exactly like a POSIX inode swap (codex P1, the
// deferred-publication identity window, PRRT_kwDORn9KaM6m9ae4). The handle
// result is cross-checked against the probed record (attributes, size, last
// write): the returned key must name the same directory entry the lookup
// described, so a wrapper resolving path through another namespace degrades
// instead of pinning a foreign file's key. FILE_FLAG_OPEN_REPARSE_POINT keeps
// the capture bound to the named directory entry: a link swapped into the
// path inside the lookup→open window opens as the link object and its
// attributes report a reparse point, which deliberately degrades instead of
// capturing the link TARGET's key. In-memory filesystems (Sys()==nil) and
// handles whose server exposes no file key degrade to not-OK, keeping the
// size+modtime legs exactly like the POSIX in-memory posture. The fs leg is
// unused: the path names the object on every OsFs-compatible fs, and the
// gating record travels with the FileInfo.
func BoundObjectIdentity(_ afero.Fs, path string, info os.FileInfo) (device, inode uint64, ok bool) {
	if info == nil {
		return 0, 0, false
	}
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
