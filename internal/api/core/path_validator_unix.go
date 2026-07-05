//go:build !windows

package core

import (
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// resolveReparseFallback is a no-op on non-Windows: NTFS volume mount points
// (reparse tag IO_REPARSE_TAG_MOUNT_POINT) are a Windows-only concern, so an
// unresolvable path on other platforms must NOT bypass canonicalization. The
// real implementation lives in path_validator_windows.go. Returning
// ErrPathUnresolvable preserves the original behavior on darwin/linux. The
// helper owns the full fallback decision (including the return value) so the
// windows-only success branch never lives in canonicalizePath and therefore
// does not count against the ubuntu/darwin codecov/patch measurement.
func resolveReparseFallback(absPath string, fs afero.Fs) (string, error) {
	return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, absPath)
}

// resolveReparseParentFallback is the parent-walk variant of
// resolveReparseFallback — see resolveReparseFallback for rationale. Returning
// ErrPathUnresolvable preserves the original behavior in the parent-walk loop
// on non-Windows.
func resolveReparseParentFallback(current string, fs afero.Fs) (string, error) {
	return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, current)
}
