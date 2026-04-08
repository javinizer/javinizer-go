package apperrors

import (
	"errors"
	"net/http"
)

type ErrorCode string

const (
	CodeAllowedDirsEmpty   ErrorCode = "ALLOWED_DIRS_EMPTY"
	CodePathOutsideAllowed ErrorCode = "PATH_OUTSIDE_ALLOWED_DIRS"
	CodePathInDenylist     ErrorCode = "PATH_IN_DENYLIST"
	CodePathNotExist       ErrorCode = "PATH_NOT_EXIST"
	CodePathNotDir         ErrorCode = "PATH_NOT_DIR"
	CodePathInvalid        ErrorCode = "PATH_INVALID"
	CodePathUnresolvable   ErrorCode = "PATH_UNRESOLVABLE"
	CodeUNCPathBlocked     ErrorCode = "UNC_PATH_BLOCKED"
	CodeReservedDeviceName ErrorCode = "RESERVED_DEVICE_NAME"
)

type PathError struct {
	Code            ErrorCode
	Message         string
	OperatorMessage string
	HTTPStatus      int
	DocsURL         string
	Path            string
}

func (e *PathError) Error() string { return e.Message }

func (e *PathError) Is(target error) bool {
	t, ok := target.(*PathError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

var (
	// ErrInodeExtraction is returned when file identity cannot be extracted
	ErrInodeExtraction = errors.New("cannot extract file identity")

	// ErrInodeMismatch is returned when inode verification fails (possible symlink swap)
	ErrInodeMismatch = &PathError{
		Code:            ErrorCode("INODE_MISMATCH"),
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

func IsPathError(err error) bool {
	var pathErr *PathError
	return errors.As(err, &pathErr)
}
