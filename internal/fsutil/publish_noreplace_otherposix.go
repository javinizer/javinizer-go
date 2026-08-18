//go:build !windows && !linux

package fsutil

// publishNoReplaceOSFS uses the hard-link publish on non-Linux POSIX
// (Darwin included — golang.org/x/sys exposes no renameatx_np wrapper, and
// link(2)'s atomic EEXIST on an occupied destination is exactly the
// no-replace semantics renameat2(RENAME_NOREPLACE) gives Linux). See
// publishNoReplaceFallback for the occupied/error posture.
func publishNoReplaceOSFS(src, dst string) error {
	return publishNoReplaceFallback(src, dst)
}
