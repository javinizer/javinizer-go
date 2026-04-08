//go:build unix || darwin || linux || freebsd || netbsd || openbsd

package core

import (
	"os"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
)

// fileIdentity represents a unique file identifier (device + inode)
type fileIdentity struct {
	device uint64
	inode  uint64
}

// getFileIdentity extracts device and inode from os.FileInfo on Unix systems
func getFileIdentity(info os.FileInfo) (fileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, apperrors.ErrInodeExtraction
	}
	return fileIdentity{
		device: uint64(stat.Dev),
		inode:  uint64(stat.Ino),
	}, nil
}

// getFileIdentityFromFd extracts device and inode from an open file on Unix systems
func getFileIdentityFromFd(f *os.File) (fileIdentity, error) {
	info, err := f.Stat()
	if err != nil {
		return fileIdentity{}, err
	}
	return getFileIdentity(info)
}
