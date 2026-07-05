//go:build !windows

package core

import "github.com/spf13/afero"

// resolveReparseFallback is a no-op on non-Windows: NTFS volume mount points
// (reparse tag IO_REPARSE_TAG_MOUNT_POINT) are a Windows-only concern, so an
// unresolvable path on other platforms must NOT bypass canonicalization. The
// real implementation lives in path_validator_windows.go. Returning ok=false
// preserves the original ErrPathUnresolvable behavior on darwin/linux.
func resolveReparseFallback(absPath string, fs afero.Fs) (string, bool) {
	return "", false
}

// resolveReparseParentFallback is a no-op on non-Windows — see
// resolveReparseFallback for rationale. Returning ok=false preserves the
// original ErrPathUnresolvable behavior in the parent-walk loop.
func resolveReparseParentFallback(current string, fs afero.Fs) (string, bool) {
	return "", false
}
