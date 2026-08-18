package fsutil

import (
	"errors"
	"fmt"

	"github.com/spf13/afero"
)

// ErrReplaceUnsupported classifies a VIRTUAL (non-OsFs) filesystem's refusal
// of the rename-over step on platforms whose abstract rename cannot express
// atomic replace (Windows MoveFileW semantics). It is chained with %w
// alongside the filesystem's own error so both stay unwrap-reachable.
var ErrReplaceUnsupported = errors.New("replace existing destination unsupported")

// replaceFileVirtualFallback is ReplaceFile's non-OsFs leg: OsFs routes to the
// native atomic-replace primitive (replacefile_windows.go MoveFileEx), while a
// virtual filesystem goes through its own rename here. The wrap uses %w for
// BOTH operands — wave-12 formatted the filesystem error with %v, which kept
// the text but dropped the original error from the unwrap chain, so the
// takeover-restore leg's wedge sentinel failed errors.Is on Windows CI (w28
// restore-rename). Host tests exercise this leg directly through the shared
// helper (Windows CI failure fixed in wave 13).
//
//nolint:unused // wired in the windows-tagged ReplaceFile (replacefile_windows.go); host-GOOS lint cannot see the cross-platform use.
func replaceFileVirtualFallback(fs afero.Fs, src, dst string) error {
	if err := fs.Rename(src, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrReplaceUnsupported, err)
	}
	return nil
}
