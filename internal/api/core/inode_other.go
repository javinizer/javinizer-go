//go:build !unix && !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows

package core

import (
	"os"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
)

// fileIdentity represents a unique file identifier (empty on unsupported platforms)
type fileIdentity struct{}

// getFileIdentity returns an error on unsupported platforms
func getFileIdentity(info os.FileInfo) (fileIdentity, error) {
	return fileIdentity{}, apperrors.ErrInodeExtraction
}

// getFileIdentityFromFd returns an error on unsupported platforms
func getFileIdentityFromFd(f *os.File) (fileIdentity, error) {
	return fileIdentity{}, apperrors.ErrInodeExtraction
}
