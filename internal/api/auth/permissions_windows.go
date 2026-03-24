//go:build windows

package auth

func enforceCredentialFilePermissions(_ string) error {
	// Windows uses ACLs instead of POSIX permission bits. Keep default ACL behavior.
	return nil
}
