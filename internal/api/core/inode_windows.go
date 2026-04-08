//go:build windows

package core

import (
	"os"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
)

// fileIdentity represents a unique file identifier (volume serial + file index)
type fileIdentity struct {
	volumeSerialNumber uint32
	fileIndexHigh      uint32
	fileIndexLow       uint32
}

// getFileIdentity extracts file ID from os.FileInfo on Windows.
//
// Windows FileInfo doesn't expose file index via Sys(), so we cannot obtain
// identity without an open handle. This function returns ErrInodeExtraction.
//
// SECURITY NOTE: On Windows, symlink-swap protection has limitations:
//   - Windows symlinks/junctions may require elevated privileges to create
//     (Developer Mode enables unprivileged symlink creation)
//   - Mandatory locking prevents concurrent manipulation of open files
//   - The open handle references the actual file object, providing post-open TOCTOU
//     protection (subsequent reads/writes use the handle, not the path)
//   - However, we CANNOT detect a swap between os.Stat() and os.Open() because
//     pre-open identity is unavailable on Windows
//
// The caller should use getFileIdentityFromFd() for post-open verification.
func getFileIdentity(info os.FileInfo) (fileIdentity, error) {
	return fileIdentity{}, apperrors.ErrInodeExtraction
}

// getFileIdentityFromFd extracts file ID from an open file handle on Windows.
//
// This uses GetFileInformationByHandle to obtain the volume serial number and
// file index (unique identifier on Windows NTFS/FAT). This works reliably on
// open file handles.
//
// Note: On Windows, the pre-open/post-open identity comparison is skipped because
// we cannot obtain pre-open identity. The open handle provides post-open TOCTOU
// protection (operations use the handle, not the path). However, a swap between
// os.Stat() and os.Open() cannot be detected on Windows.
func getFileIdentityFromFd(f *os.File) (fileIdentity, error) {
	var info syscall.ByHandleFileInformation
	err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info)
	if err != nil {
		return fileIdentity{}, err
	}
	return fileIdentity{
		volumeSerialNumber: info.VolumeSerialNumber,
		fileIndexHigh:      info.FileIndexHigh,
		fileIndexLow:       info.FileIndexLow,
	}, nil
}
