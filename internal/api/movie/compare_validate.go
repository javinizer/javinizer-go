package movie

import (
	"errors"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/spf13/afero"
)

// Sentinel errors for NFO validation
var (
	ErrNFONotFound     = errors.New("nfo file not found")
	ErrNFOAccessDenied = errors.New("access denied: path is outside allowed directories")
	ErrNFOInvalidPath  = errors.New("invalid file path")
	ErrNFOIsDirectory  = errors.New("path is a directory, not a file")
)

// validateNFOPath validates an NFO file path against security constraints
// Returns the validated absolute path or a sentinel error
func validateNFOPath(requestedPath string, allowedDirs []string) (string, error) {
	v := core.NewPathValidator(afero.NewOsFs(), allowedDirs, nil)
	canonicalPath, err := v.ValidateFile(requestedPath)
	if err != nil {
		// Map PathValidator errors to the sentinel errors expected by callers
		switch {
		case apperrors.IsPathError(err, apperrors.ErrAllowedDirsEmpty):
			return "", ErrNFOAccessDenied
		case apperrors.IsPathError(err, apperrors.ErrPathOutsideAllowed):
			return "", ErrNFOAccessDenied
		case apperrors.IsPathError(err, apperrors.ErrPathInDenylist):
			return "", ErrNFOAccessDenied
		case apperrors.IsPathError(err, apperrors.ErrPathNotExist):
			return "", ErrNFONotFound
		case apperrors.IsPathError(err, apperrors.ErrPathNotFile):
			return "", ErrNFOIsDirectory
		default:
			return "", ErrNFOInvalidPath
		}
	}
	return canonicalPath, nil
}
