//go:build !windows

package core

func resolveShortPathName(path string) string {
	// No-op on non-Windows platforms
	return path
}
