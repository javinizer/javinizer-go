//go:build !windows

package workflow

import "github.com/spf13/afero"

// openSwapPinnedSource is the swap wrapper's POSIX pin: rename/unlink of an
// open entry is permissive there, so the plain filesystem open already pins
// the admitted object across the simulated mid-flight swap — the descriptor
// keeps addressing it while the name moves aside and the replacement lands
// fresh. The Windows twin
// (artifact_staging_verified_source_open_windows_test.go) must ask for the
// delete share explicitly, or the swap's rename would be refused against the
// wrapper's own pin and the rehearsal would degrade into states the
// production delete-shared pin never produces.
func openSwapPinnedSource(fs afero.Fs, name string) (afero.File, error) {
	return fs.Open(name)
}
