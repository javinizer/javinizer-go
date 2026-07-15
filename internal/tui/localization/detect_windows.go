//go:build windows

package localization

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// localeNameMaxLen is LOCALE_NAME_MAX_LENGTH from winnt.h.
const localeNameMaxLen = 85

// getUserDefaultLocaleName calls the Win32 GetUserDefaultLocaleName API and
// returns the user's default locale as a BCP 47 tag (e.g. "ja-JP"). It returns
// "" on any failure so callers fall back to English.
func getUserDefaultLocaleName() string {
	buf := make([]uint16, localeNameMaxLen)
	r1, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(localeNameMaxLen),
	)
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// detectPlatformLocale resolves the OS locale preference list on Windows. Any
// explicitly set POSIX env vars (LANG/LC_*/LANGUAGE, common under Git Bash or
// MSYS shells) take precedence; otherwise the Win32 user default locale name
// (already BCP 47) is used.
func detectPlatformLocale() []string {
	if envTags := detectFromEnv(os.Getenv); len(envTags) > 0 {
		return envTags
	}
	if name := getUserDefaultLocaleName(); name != "" {
		return []string{name}
	}
	return nil
}
