//go:build windows

package core

import (
	"golang.org/x/sys/windows"
)

func resolveShortPathName(path string) string {
	return resolveShortPathNameWith(path, windows.GetLongPathName)
}

// resolveShortPathNameWith keeps each native lookup local to one resolution.
// The filesystem can change between the sizing call and its retry.
func resolveShortPathNameWith(path string, getLongPathName func(*uint16, *uint16, uint32) (uint32, error)) string {
	if path == "" {
		return path
	}

	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}

	buf := make([]uint16, windows.MAX_PATH)
	n, err := getLongPathName(ptr, &buf[0], uint32(len(buf)))
	if err == nil && n > uint32(len(buf)) {
		buf = make([]uint16, n+1)
		n, err = getLongPathName(ptr, &buf[0], uint32(len(buf)))
	}

	if err != nil || n == 0 {
		return path
	}

	return windows.UTF16PtrToString(&buf[0])
}
