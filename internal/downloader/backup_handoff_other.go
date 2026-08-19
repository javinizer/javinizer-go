//go:build !linux

package downloader

import (
	"os"

	"github.com/spf13/afero"
)

// handoffToReservedBackup is the reserved-backup handoff on platforms without
// a renameat2-style atomic-exchange primitive (wave-37/wave-38, codex P2,
// PR#215): Windows (ReplaceFile/MoveFileEx replace-by-design) and non-Linux
// POSIX (golang.org/x/sys exposes no renameatx_np wrapper for Darwin/BSD —
// the same constraint fsutil's publish_noreplace_otherposix.go documents).
// The closed shape is the wave-38 CONDITIONAL take-aside (finding F2):
// handoffViaVerifiedRename takes the reservation placeholder aside onto a
// bound scratch first, moves dest onto the freed backup name NO-REPLACE, and
// unlinks only the scratch re-bound against the claim.
func handoffToReservedBackup(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) error {
	return handoffViaVerifiedRename(fsys, destPath, backupPath, claim)
}
