//go:build windows

package history

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

func primaryUndoCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}

// The bounded implementation does not yet establish a durable Windows
// ancestor namespace barrier or handle-bound readonly cleanup. Refuse before
// admission, and on every retry, rather than weaken the copy-first guarantee.
func primaryUndoDurabilitySupported() error {
	return fmt.Errorf("cross-volume primary Undo on Windows requires manual recovery: durable namespace and handle-bound cleanup are not supported; library source retained")
}
func primaryUndoSyncDir(string) error { return primaryUndoDurabilitySupported() }

var primaryUndoMoveFile = windows.MoveFileEx

func primaryUndoPublishNoReplace(fs afero.Fs, source, dest string) error {
	if _, ok := fs.(*afero.OsFs); !ok {
		return fsutil.PublishNoReplace(fs, source, dest)
	}
	src, err := windows.UTF16PtrFromString(restoreSourceLongPath(source))
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(restoreSourceLongPath(dest))
	if err != nil {
		return err
	}
	// Both endpoints are on the same volume: the payload was already copied
	// to a destination sibling. In particular, neither COPY_ALLOWED nor
	// REPLACE_EXISTING is permitted here.
	err = primaryUndoMoveFile(src, dst, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%w: %s", fsutil.ErrPublishCollision, dest)
	}
	return err
}

// FileIdInfo is required: the older 64-bit BY_HANDLE_FILE_INFORMATION key
// is not guaranteed unique on ReFS. Unsupported filesystems fail closed.
var primaryUndoGetFileInfo = windows.GetFileInformationByHandleEx

type primaryUndoFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

func primaryUndoObjectKey(file afero.File, _ os.FileInfo) (uint64, uint64, string, bool) {
	native, ok := file.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, "", false
	}
	var info primaryUndoFileIDInfo
	if err := primaryUndoGetFileInfo(windows.Handle(native.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return 0, 0, "", false
	}
	return info.VolumeSerialNumber, 0, hex.EncodeToString(info.FileID[:]), true
}

// FILE_BASIC_INFO's ChangeTime is a local mutation witness, separate from
// persistent identity: renames legitimately change it.
type primaryUndoFileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
	FileAttributes                                          uint32
	Padding                                                 uint32
}

func primaryUndoObserveChange(file afero.File) (primaryUndoChangeWitness, error) {
	native, ok := file.(interface{ Fd() uintptr })
	if !ok {
		return primaryUndoChangeWitness{}, fmt.Errorf("primary Undo requires a native mutation witness")
	}
	var info primaryUndoFileBasicInfo
	if err := primaryUndoGetFileInfo(windows.Handle(native.Fd()), windows.FileBasicInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return primaryUndoChangeWitness{}, err
	}
	return primaryUndoChangeWitness{Seconds: info.ChangeTime}, nil
}
