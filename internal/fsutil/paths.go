package fsutil

import "path/filepath"

// NormalizePath converts a file path to a canonical slash-separated form.
// On Windows, it strips the drive letter prefix (e.g., "C:") and converts
// backslashes to forward slashes. On other platforms, it just normalizes
// to forward slashes.
func NormalizePath(p string) string {
	if len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
		p = p[2:]
	}
	p = filepath.ToSlash(p)
	return p
}
