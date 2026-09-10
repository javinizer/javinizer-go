//go:build !windows

package fsutil

import (
	"fmt"
	"syscall"
)

// noClobberDirIdentity fingerprints a directory's hosting filesystem: dev+ino
// both change on unmount/remount at the same absolute path and a symlink
// retarget to another volume resolves to the new device, so probe cache keys
// carrying this fingerprint can never leak a verdict across mounts (codex P2,
// PR #255). Empty string signals "identity unknown" and the caller degrades
// to path-only caching.
func noClobberDirIdentity(path string) string {
	info, err := noClobberProbeStat(path)
	if err != nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	// Dev/Ino widths differ across unix targets (int32 on darwin, uint64 on
	// linux), so format the raw values: any explicit conversion is flagged as
	// unnecessary by unconvert on the target where the field is already wide.
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino)
}
