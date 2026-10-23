//go:build !windows && !linux && !darwin

package fsutil

func publishNoReplaceOSFS(src, dst string) error {
	return publishNoReplaceFallback(src, dst)
}
