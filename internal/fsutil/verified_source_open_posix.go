//go:build !windows

package fsutil

import "github.com/spf13/afero"

// openVerifiedSource opens the verified composites' source handle. POSIX
// unlink/rename of an open entry is permissive, so the plain filesystem open
// already admits the mid-verification name swap the composite is pinned
// against — the descriptor keeps addressing the admitted object while the
// directory entry moves. The Windows twin (verified_source_open_windows.go)
// must ask for the delete share explicitly.
func openVerifiedSource(fs afero.Fs, path string) (afero.File, error) {
	return fs.Open(path)
}
