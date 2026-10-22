//go:build !windows

package workflow

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/logging"
)

const artifactSweepKeyNoFollow = syscall.O_NOFOLLOW

var (
	artifactSweepGeteuid  = os.Geteuid
	artifactSweepKeyChmod = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
	artifactSweepKeyWarns sync.Map
)

// secureArtifactSweepSecret enforces the key's owner-only contract on the
// opened handle. A key owned by another account is refused outright: anyone
// able to plant it can mint seals. Group/other permission bits are tightened
// to 0600 through the handle; when the filesystem refuses mode changes
// (ACL-managed volumes such as ZFS aclmode=restricted), the ACL is
// authoritative and the key is used with a one-time warning, because
// refusing it would only disable staging cleanup without protecting the key.
func secureArtifactSweepSecret(path string, f *os.File, info os.FileInfo) error {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if euid := artifactSweepGeteuid(); int(st.Uid) != euid {
			return fmt.Errorf("artifact sweep key %s is owned by uid %d, not the running uid %d", path, st.Uid, euid)
		}
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := artifactSweepKeyChmod(f, 0o600); err != nil {
		if !artifactSweepKeyModeRefused(err) {
			return fmt.Errorf("restrict artifact sweep key %s permissions: %w", path, err)
		}
		if _, warned := artifactSweepKeyWarns.LoadOrStore(path, struct{}{}); !warned {
			logging.Warnf("artifact sweep key %s is accessible beyond its owner (mode %v) and the filesystem refused tightening it (%v); restrict the directory ACL to the Javinizer user", path, info.Mode().Perm(), err)
		}
		return nil
	}
	logging.Warnf("artifact sweep key %s was accessible beyond its owner (mode %v); restricted to 0600", path, info.Mode().Perm())
	return nil
}

func artifactSweepKeyModeRefused(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)
}
