//go:build !windows

package localization

import "os"

// detectPlatformLocale resolves the OS locale preference list on POSIX systems
// from the LANGUAGE, LC_ALL, LC_MESSAGES, and LANG environment variables.
func detectPlatformLocale() []string {
	return detectFromEnv(os.Getenv)
}
