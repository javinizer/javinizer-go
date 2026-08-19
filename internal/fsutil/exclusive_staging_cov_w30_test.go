//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-30 — CloseStaged leg coverage
// that wave-29 left unexercised (its POSIX POSIX-leg callers moved onto
// PublishStagedBound, but CloseStaged remains the virtual/wrapper tail and
// every leg stays pinned): an ENOSYS fd-times answer defers to the
// name-based leg, and a handle-close failure returns the close error.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// ENOSYS from the fd times seam (platforms without an fd-scoped wrapper —
// staging_times_unixother.go's documented posture) defers to the post-close
// name-based Chtimes leg even on the real OsFs.
func TestCloseStagedW30_ENOSYSDefersToNameLeg(t *testing.T) {
	prev := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prev })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)

	ancient := time.Unix(946684800, 0)
	require.NoError(t, CloseStaged(fs, staged, fh, ancient, ancient, true))
	info, err := os.Stat(staged)
	require.NoError(t, err)
	require.Equal(t, ancient.Unix(), info.ModTime().Unix(),
		"the deferred name-based leg landed the times")
}

// StagingTimesError carries its message end-to-end (callers render it in
// the "... times" wrap): pin the Error/flattening so the typed value stays
// printable, and its Unwrap keeps the underlying error reachable.
func TestStagingTimesErrorW30_RendersAndUnwraps(t *testing.T) {
	inner := errors.New("disk reports times wedged")
	e := &StagingTimesError{Staged: "/x/poster.jpg.rstr.1", Err: inner}
	require.Equal(t, "apply staged times for /x/poster.jpg.rstr.1: disk reports times wedged", e.Error())
	require.ErrorIs(t, e, inner)
}

// A close failure surfaces as the raw close class: callers distinguish it
// from the typed *StagingTimesError exactly like pre-wave-30 CloseStaged.
func TestCloseStagedW30_CloseFailureSurfacesRaw(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)
	require.NoError(t, fh.Close(), "pre-close so CloseStaged's own close must fail")

	ancient := time.Unix(946684800, 0)
	cerr := CloseStaged(fs, staged, fh, ancient, ancient, false)
	require.Error(t, cerr)
	var timesErr *StagingTimesError
	require.NotErrorAs(t, cerr, &timesErr, "a close failure is never classified as a times-leg failure")
}
