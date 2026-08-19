//go:build windows

package fsutil

import (
	"os"

	"github.com/spf13/afero"
)

// restoreStagingMode attempts to apply a staging file's requested permission
// bits exactly. Windows models POSIX modes only coarsely (the read-only
// attribute; group/other bits do not map onto ACLs), so the real-OsFs leg
// — applied THROUGH THE OPEN HANDLE (wave-29: never the staged path, so a
// planted name cannot redirect it) — mirrors the repo's ACL best-effort
// style: a failed handle Chmod is ignored and never fails the staging
// attempt.
//
// The VIRTUAL-filesystem fallback is different: test doubles implement
// Chmod deterministically (afero MemMapFs against stagedVirtualModePath's
// stored spelling normalization), so a refusal there is a genuine staging
// failure on every host and surfaces — keeping the wave-9 wedged-chmod
// expectation host-portable instead of silently passing on Windows.
func restoreStagingMode(fs afero.Fs, staged string, fh afero.File, perm os.FileMode) error {
	if of, ok := osStagingHandle(fs, fh); ok {
		_ = stagedHandleChmod(of, perm)
		return nil
	}
	return fs.Chmod(stagedVirtualModePath(staged), perm)
}

// RestoreStagingOwnership is a no-op on Windows: ownership is modeled by ACL
// entries, not uid/gid, and the fd-scoped chown has no Windows
// implementation. The staged inode keeps the restoring account's identity.
func RestoreStagingOwnership(fs afero.Fs, staged afero.File, source os.FileInfo) {}
