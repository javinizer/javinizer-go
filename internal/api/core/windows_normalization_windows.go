//go:build windows

package core

import (
	"golang.org/x/sys/windows"
)

func resolveShortPathName(path string) string {
	if path == "" {
		return path
	}

	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}

	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.GetLongPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil {
		return path
	}

	if n > uint32(len(buf)) {
		buf = make([]uint16, n+1)
		n, err = windows.GetLongPathName(ptr, &buf[0], uint32(len(buf)))
		if err != nil {
			return path
		}
	}

	if n == 0 {
		return path
	}

	return windows.UTF16PtrToString(&buf[0])
}
