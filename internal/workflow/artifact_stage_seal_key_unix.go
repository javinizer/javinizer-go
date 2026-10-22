//go:build !windows

package workflow

import (
	"fmt"
	"os"
	"syscall"
)

// O_NONBLOCK lets us reject FIFOs before a read-only open can wait for a writer.
const artifactSweepKeyReadFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

var (
	artifactSweepGeteuid      = os.Geteuid
	secureArtifactSweepSecret = secureArtifactSweepSecretPOSIX
)

// secureArtifactSweepSecretPOSIX refuses unsafe or unverifiable metadata.
// Never repair an existing key: restricting it now cannot undo past exposure.
// In particular, chmod refusal is not evidence that an ACL protects the key.
// Owner-only mode is necessary, but independent ACL privacy remains a trusted
// private-cache deployment assumption (see artifact_stage_seal.go).
func secureArtifactSweepSecretPOSIX(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return fmt.Errorf("cannot verify artifact sweep key %s ownership", path)
	}
	if euid := artifactSweepGeteuid(); int(st.Uid) != euid {
		return fmt.Errorf("artifact sweep key %s is owned by uid %d, not the running uid %d", path, st.Uid, euid)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("artifact sweep key %s is accessible beyond its owner (mode %v); refusing potentially exposed key", path, info.Mode().Perm())
	}
	return nil
}
