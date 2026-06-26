//go:build windows

package auth

import "github.com/spf13/afero"

func enforceCredentialFilePermissions(_ afero.Fs, _ string) error {
	// Windows uses ACLs instead of POSIX permission bits. Keep default ACL behavior.
	return nil
}
