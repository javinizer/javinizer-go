//go:build !linux

package downloader

import (
	"os"

	"github.com/spf13/afero"
)

// handoffToReservedBackup is the reserved-backup handoff on platforms without
// a renameat2-style atomic-exchange primitive (wave-37, codex P2, PR#215):
// Windows (ReplaceFile/MoveFileEx replace-by-design) and non-Linux POSIX
// (golang.org/x/sys exposes no renameatx_np wrapper for Darwin/BSD — the same
// constraint fsutil's publish_noreplace_otherposix.go documents). The closed
// shape here is the codex-accepted identity-bound one: re-derive the
// reservation at syscall adjacency immediately BEFORE the replacing rename,
// and bind the failure cleanup to the claimed placeholder
// (handoffViaVerifiedRename).
func handoffToReservedBackup(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) error {
	return handoffViaVerifiedRename(fsys, destPath, backupPath, claim)
}
