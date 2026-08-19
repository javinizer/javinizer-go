//go:build windows

package fsutil

import (
	"time"

	"golang.org/x/sys/windows"
)

// stagedHandleChtimes sets atime/mtime through the open staged file handle
// via SetFileTime (wave-29, codex P1, PR#215): a name planted mid-flow can
// never redirect the metadata. The staging handles open with GENERIC_WRITE,
// which includes FILE_WRITE_ATTRIBUTES (the access SetFileTime requires).
// Exposed as a test seam so tests can record the handle+times.
var stagedHandleChtimes = func(handle uintptr, atime, mtime time.Time) error {
	at := windows.NsecToFiletime(atime.UnixNano())
	mt := windows.NsecToFiletime(mtime.UnixNano())
	return windows.SetFileTime(windows.Handle(handle), nil, &at, &mt)
}
