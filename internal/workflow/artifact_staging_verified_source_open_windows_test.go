//go:build windows

package workflow

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// openSwapPinnedSource opens the swap wrapper's pin handle the way the
// production verified source opener pins a real OsFs source on Windows
// (fsutil.openVerifiedSource): with the full sharing vocabulary read | write |
// DELETE. Go's os.Open requests only FILE_SHARE_READ|FILE_SHARE_WRITE
// (syscall.Open's fixed sharemode), and Windows relocation is
// refuse-while-pinned: MoveFileEx answers ERROR_ACCESS_DENIED whenever any
// handle on the entry omits the delete share. A wrapper rehearsing the
// mid-open swap through such a handle would see its rename refused and its
// replacement plant rewrite the PINNED object in place — no aside ever
// exists, and the streamed handle carries foreign bytes the admission proof
// then refuses on the size/modtime legs — a state the production
// delete-shared pin can never produce. OsFs inners therefore open through
// CreateFile below; virtual inners keep the plain interface open (their
// semantics are the test host's, not the Windows ABI's), mirroring the
// production leg's posture. FILE_FLAG_BACKUP_SEMANTICS keeps os.Open parity
// for directory entries, and the handle is closed on every post-CreateFile
// failure branch so nothing outlives this call on error.
func openSwapPinnedSource(fs afero.Fs, name string) (afero.File, error) {
	if _, ok := fs.(*afero.OsFs); !ok {
		return fs.Open(name)
	}
	p, perr := windows.UTF16PtrFromString(name)
	if perr != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: perr}
	}
	handle, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, &os.PathError{Op: "open", Path: name, Err: syscall.EINVAL}
	}
	return file, nil
}
