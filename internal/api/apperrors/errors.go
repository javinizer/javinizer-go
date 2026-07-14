package apperrors

import (
	"errors"
	"net/http"
)

type errorCode string

// errorCode values identify each path-validation failure returned to API callers.
const (
	CodeAllowedDirsEmpty   errorCode = "ALLOWED_DIRS_EMPTY"
	CodePathOutsideAllowed errorCode = "PATH_OUTSIDE_ALLOWED_DIRS"
	CodePathInDenylist     errorCode = "PATH_IN_DENYLIST"
	CodePathNotExist       errorCode = "PATH_NOT_EXIST"
	CodePathNotDir         errorCode = "PATH_NOT_DIR"
	CodePathNotFile        errorCode = "PATH_NOT_FILE"
	CodePathInvalid        errorCode = "PATH_INVALID"
	CodePathUnresolvable   errorCode = "PATH_UNRESOLVABLE"
	CodeUNCPathBlocked     errorCode = "UNC_PATH_BLOCKED"
	CodeReservedDeviceName errorCode = "RESERVED_DEVICE_NAME"
)

// PathError describes a filesystem path error returned to API callers, carrying a stable code and HTTP status.
type PathError struct {
	Code            errorCode
	Message         string
	OperatorMessage string
	HTTPStatus      int
	DocsURL         string
	Path            string
}

func (e *PathError) Error() string { return e.Message }

// Is reports whether target is a PathError with the same error code.
func (e *PathError) Is(target error) bool {
	t, ok := target.(*PathError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Sentinel errors returned by path validation and file identity checks.
var (
	// ErrInodeExtraction is returned when file identity cannot be extracted
	ErrInodeExtraction = errors.New("cannot extract file identity")

	// ErrInodeMismatch is returned when inode verification fails (possible symlink swap)
	ErrInodeMismatch = &PathError{
		Code:            errorCode("INODE_MISMATCH"),
		Message:         "security violation: file identity changed",
		OperatorMessage: "File was replaced or swapped after validation (possible symlink attack)",
		HTTPStatus:      http.StatusInternalServerError,
		DocsURL:         "",
	}

	ErrAllowedDirsEmpty = &PathError{
		Code:            CodeAllowedDirsEmpty,
		Message:         "access denied: no allowed directories configured",
		OperatorMessage: "Add directories to api.security.allowed_directories in your configuration file",
		HTTPStatus:      http.StatusForbidden,
		DocsURL:         "/docs/configuration#security",
	}

	ErrPathOutsideAllowed = &PathError{
		Code:            CodePathOutsideAllowed,
		Message:         "access denied: path outside allowed directories",
		OperatorMessage: "Check that the path is within a configured allowed directory",
		HTTPStatus:      http.StatusForbidden,
		DocsURL:         "/docs/configuration#security",
	}

	ErrPathInDenylist = &PathError{
		Code:            CodePathInDenylist,
		Message:         "access denied: cannot scan system directory",
		OperatorMessage: "This directory is in the built-in minimal denylist (/proc, /sys, /dev)",
		HTTPStatus:      http.StatusForbidden,
		DocsURL:         "",
	}

	ErrPathNotExist = &PathError{
		Code:            CodePathNotExist,
		Message:         "path does not exist",
		OperatorMessage: "Verify the path is correct and the directory exists",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}

	ErrPathNotDir = &PathError{
		Code:            CodePathNotDir,
		Message:         "path is not a directory",
		OperatorMessage: "Provide a directory path, not a file path",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}

	ErrPathNotFile = &PathError{
		Code:            CodePathNotFile,
		Message:         "path is not a regular file",
		OperatorMessage: "Provide a file path, not a directory path",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}

	ErrPathInvalid = &PathError{
		Code:            CodePathInvalid,
		Message:         "cannot access path",
		OperatorMessage: "The path contains invalid characters or format, or cannot be accessed",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}

	ErrPathUnresolvable = &PathError{
		Code:            CodePathUnresolvable,
		Message:         "cannot resolve path",
		OperatorMessage: "Path may contain broken symlinks or circular references",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}

	ErrUNCPathBlocked = &PathError{
		Code:            CodeUNCPathBlocked,
		Message:         "access denied: UNC paths are not allowed",
		OperatorMessage: "UNC paths (\\\\server\\share) can leak NTLM credentials. Enable api.security.allow_unc and configure allowed servers if needed.",
		HTTPStatus:      http.StatusForbidden,
		DocsURL:         "/docs/configuration#security",
	}

	ErrReservedDeviceName = &PathError{
		Code:            CodeReservedDeviceName,
		Message:         "invalid path: reserved device name",
		OperatorMessage: "Path contains Windows reserved device name (CON, NUL, COM1-9, LPT1-9, etc.)",
		HTTPStatus:      http.StatusBadRequest,
		DocsURL:         "",
	}
)

// NewPathError returns a copy of base with the offending path attached.
func NewPathError(base *PathError, path string) *PathError {
	return &PathError{
		Code:            base.Code,
		Message:         base.Message,
		OperatorMessage: base.OperatorMessage,
		HTTPStatus:      base.HTTPStatus,
		DocsURL:         base.DocsURL,
		Path:            path,
	}
}

// IsPathError checks whether err is a PathError with the given code.
// This enables callers to match specific path error types without
// depending on the internal PathError pointer identity.
func IsPathError(err error, target *PathError) bool {
	if err == nil || target == nil {
		return false
	}
	var pe *PathError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Code == target.Code
}
