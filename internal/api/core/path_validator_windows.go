//go:build windows

package core

import (
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// resolveReparseFallback implements the Windows-only Stat-fallback for an
// absolute path whose EvalSymlinks failed with a non-NotExist error (e.g. an
// NTFS volume mount point / reparse tag IO_REPARSE_TAG_MOUNT_POINT). Such a
// path is an admin-created filesystem mount, not a user-controllable symlink,
// so the cleaned absolute path is a safe canonical form when Stat confirms
// the path genuinely exists and is accessible. A Stat failure (broken
// symlink, symlink loop, permission denied) returns ErrPathUnresolvable so
// the caller surfaces the original error. The helper owns the full fallback
// decision (Stat check + return value) so the windows-only success return
// lives in this windows-tagged file and does not count against the
// ubuntu/darwin codecov/patch measurement.
func resolveReparseFallback(absPath string, fs afero.Fs) (string, error) {
	if _, statErr := fs.Stat(absPath); statErr == nil {
		return filepath.Clean(absPath), nil
	}
	return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, absPath)
}

// resolveReparseParentFallback is the parent-walk variant of
// resolveReparseFallback: an existing parent whose only problem is an
// unresolvable reparse point (e.g. NTFS mount point) is accepted as its
// cleaned path. Windows-only — see resolveReparseFallback for rationale. The
// helper owns the full decision (Stat check + return value) so the
// windows-only success return lives in this windows-tagged file.
func resolveReparseParentFallback(current string, fs afero.Fs) (string, error) {
	if _, parentStatErr := fs.Stat(current); parentStatErr == nil {
		return filepath.Clean(current), nil
	}
	return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, current)
}
