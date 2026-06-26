//go:build !windows

package auth

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/afero"
)

const credentialFileMode = 0600

func enforceCredentialFilePermissions(fs afero.Fs, path string) error {
	info, err := lstatOrStat(fs, path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("credential path %s must not be a symlink", path)
	}
	if info.IsDir() {
		return fmt.Errorf("credential path %s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("credential path %s is not a regular file", path)
	}

	if info.Mode().Perm() == credentialFileMode {
		return nil
	}

	if err := fs.Chmod(path, credentialFileMode); err != nil {
		if isUnsupportedPermissionMutation(err) {
			return fmt.Errorf(
				"credential file mode is %o and filesystem does not support chmod to %o: %w",
				info.Mode().Perm(),
				credentialFileMode,
				err,
			)
		}
		return err
	}

	info, err = lstatOrStat(fs, path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("credential path %s must not be a symlink", path)
	}
	if info.Mode().Perm() != credentialFileMode {
		return fmt.Errorf("credential file mode is %o, expected %o", info.Mode().Perm(), credentialFileMode)
	}
	return nil
}

// lstatOrStat returns file info for path, preferring Lstat (via the optional
// afero.Lstater interface) so symlinks are detected rather than followed.
// OsFs.LstatIfPossible delegates to os.Lstat, preserving the prior behavior
// exactly; filesystems that don't model symlinks fall back to Stat. Routing
// stat/chmod through the injected filesystem keeps AuthManager compatible with
// non-Os Afero filesystems instead of bypassing the abstraction with raw
// os.Lstat/os.Chmod calls.
func lstatOrStat(fs afero.Fs, path string) (os.FileInfo, error) {
	if ls, ok := fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(path)
		return info, err
	}
	return fs.Stat(path)
}

func isUnsupportedPermissionMutation(err error) bool {
	return errors.Is(err, syscall.EOPNOTSUPP) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EROFS)
}
