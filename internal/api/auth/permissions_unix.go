//go:build !windows

package auth

import (
	"fmt"
	"os"
)

const credentialFileMode = 0600

func enforceCredentialFilePermissions(path string) error {
	info, err := os.Lstat(path)
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

	if err := os.Chmod(path, credentialFileMode); err != nil {
		return err
	}

	info, err = os.Lstat(path)
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
