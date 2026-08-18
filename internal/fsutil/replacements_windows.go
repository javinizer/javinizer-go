//go:build windows

package fsutil

import (
	"os"

	"github.com/spf13/afero"
)

// restoreStagingMode attempts to apply a staging file's requested permission
// bits exactly. Windows models POSIX modes only coarsely (the read-only
// attribute; group/other bits do not map onto ACLs), so this mirrors the
// repo's ACL best-effort style: a failed Chmod is ignored and never fails
// the staging attempt.
func restoreStagingMode(fs afero.Fs, path string, perm os.FileMode) error {
	_ = fs.Chmod(path, perm)
	return nil
}

// RestoreStagingOwnership is a no-op on Windows: ownership is modeled by ACL
// entries, not uid/gid, and os.Chown has no Windows implementation. The
// staged inode keeps the restoring account's identity.
func RestoreStagingOwnership(fs afero.Fs, staged string, source os.FileInfo) {}
