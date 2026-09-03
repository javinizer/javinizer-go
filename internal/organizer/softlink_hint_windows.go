//go:build windows

package organizer

// On Windows, symlinks require Developer Mode or elevated privileges.
const softLinkPermDeniedHint = " (Windows requires Developer Mode or elevated privileges for symlinks)"
