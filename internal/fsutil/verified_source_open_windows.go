//go:build windows

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// openVerifiedSource opens the verified composites' source handle with
// Windows's full sharing vocabulary (read | write | DELETE). Go's os.Open
// requests only FILE_SHARE_READ|FILE_SHARE_WRITE (syscall.Open's fixed
// sharemode), so an entry pinned by such a handle cannot be re-pointed at
// all: the mid-verification swap the composite is bound against — DeleteFile
// or rename-away of the live entry — is refused with ERROR_SHARING_VIOLATION
// / ERROR_ACCESS_DENIED while the composite is still proving and streaming
// the handle. The verified contracts pin the OBJECT through the descriptor
// precisely so the NAME may move mid-flight, which Windows can only express
// when the pin itself shares deletion. FILE_FLAG_BACKUP_SEMANTICS keeps
// os.Open parity for directory entries, and the handle is closed on every
// post-CreateFile failure branch so nothing outlives this call on error.
// Virtual filesystems keep their interface open — their semantics are the
// test host's, not the Windows ABI's.
func openVerifiedSource(fs afero.Fs, path string) (afero.File, error) {
	if _, ok := fs.(afero.OsFs); !ok {
		return fs.Open(path)
	}
	p, perr := windows.UTF16PtrFromString(path)
	if perr != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: perr}
	}
	handle, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EINVAL}
	}
	return file, nil
}
