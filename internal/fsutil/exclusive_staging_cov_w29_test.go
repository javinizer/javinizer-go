package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-29 (P1) — "apply staging metadata
// through the open file handle": CreateExclusiveStagingFile's mode re-assert,
// CloseStaged's times application, and RestoreStagingOwnership's hand-off all
// ride the open staging HANDLE on the real OsFs (fd-scoped fchmod/futimens/
// fchown), so a directory writer renaming the staged name away and planting a
// symlink mid-flow can never redirect metadata onto an arbitrary target.
// Virtual filesystems keep the name-based fallback legs against the stored
// (filepath.Clean'd) spelling, with CloseStaged applying times AFTER close on
// handle flavors that re-stamp ModTime at close/write (afero MemMapFs).
//
// The portable (build-tag-free) tests pin the virtual-fallback legs; the
// handle-seam recordings for the real OsFs live in
// exclusive_staging_cov_w29_posix_test.go.

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// CloseStaged on a virtual filesystem lands the times AFTER the handle close:
// afero's mem-style file handles re-stamp ModTime at every Write and at
// Close, so any pre-close Chtimes would be silently overwritten. The final
// staged inode must carry the requested timestamps exactly.
func TestCloseStagedW29_VirtualFsTimesLandAfterClose(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/W29-TIMES", 0o755))
	staged, fh, err := CreateExclusiveStagingFile(fs, "/out/W29-TIMES/poster.jpg", ".rstr", 1, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)

	ancient := time.Unix(946684800, 123456789)
	require.NoError(t, CloseStaged(fs, staged, fh, ancient, ancient, true))

	info, err := fs.Stat(staged)
	require.NoError(t, err)
	require.Equal(t, ancient.UnixNano(), info.ModTime().UnixNano(),
		"the requested times survive the virtual handle's close-time re-stamp")
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the create-time mode assert landed")
}

// applyTimes=false keeps the copy-only posture: no Chtimes call on any leg,
// and the close-time re-stamp stands as the (host-clock) mtime.
func TestCloseStagedW29_ApplyTimesFalseRunsNoTimesLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/out/W29-NOTIMES", 0o755))
	fs := &w29CountingChtimesFs{Fs: base}

	staged, fh, err := CreateExclusiveStagingFile(fs, "/out/W29-NOTIMES/poster.jpg", ".rstr", 1, 0o644)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)

	ancient := time.Unix(946684800, 0)
	require.NoError(t, CloseStaged(fs, staged, fh, ancient, ancient, false))
	require.Zero(t, fs.calls, "applyTimes=false must not run any times leg")

	info, err := base.Stat(staged)
	require.NoError(t, err)
	require.NotEqual(t, ancient.Unix(), info.ModTime().Unix(), "the close-stamp stands untouched")
}

// A virtual-leg times failure surfaces as the TYPED *StagingTimesError so
// callers keep their distinct times-vs-close error texts, with the requested
// inode left staged (the caller's remove leg owns cleanup).
func TestCloseStagedW29_VirtualTimesFailureIsTyped(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/out/W29-TIMESFAIL", 0o755))
	wedge := errors.New("w29 staged chtimes wedged")
	fs := &w29CountingChtimesFs{Fs: base, err: wedge}

	staged, fh, err := CreateExclusiveStagingFile(fs, "/out/W29-TIMESFAIL/poster.jpg", ".rstr", 1, 0o644)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)

	err = CloseStaged(fs, staged, fh, time.Unix(946684800, 0), time.Unix(946684800, 0), true)
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr, "the times leg is typed for the caller's error text")
	require.ErrorIs(t, err, wedge, "the wedge stays unwrap-reachable")
	require.Equal(t, staged, timesErr.Staged)
	require.Equal(t, 1, fs.calls, "exactly one times application was attempted")

	_, statErr := base.Stat(staged)
	require.NoError(t, statErr, "the staged inode survives for the caller's cleanup decision")
}

// w29CountingChtimesFs counts (and optionally wedges) Chtimes calls.
type w29CountingChtimesFs struct {
	afero.Fs
	calls int
	err   error
}

func (f *w29CountingChtimesFs) Chtimes(name string, atime, mtime time.Time) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return f.Fs.Chtimes(name, atime, mtime)
}
