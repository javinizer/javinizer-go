//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-24 (coverage fallout of the
// seam-driven wave-30 hardening): the defensive legs of PublishStagedBound
// that no host setup can fail deterministically are replayed through the
// package seams — the restream seam's own seek failure, the virtual/wrapper
// leg's CloseStaged failures, and the ENOSYS-deferred destination Chtimes
// failure. The indeterminate post-publish destination lookup itself moved
// onto publishStagedBoundDestLstat (see the wave-30 POSIX companion): chmod
// -based directory denial does not fail for uid 0 and silently reopened
// those legs on root CI hosts.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w24SeekErrFile fails Seek so publishStagedBoundRestream's probe leg is
// replayed without wedging a real descriptor.
type w24SeekErrFile struct {
	afero.File
	err error
}

func (f w24SeekErrFile) Seek(int64, int) (int64, error) { return 0, f.err }

// The production restream seam's seek leg: a descriptor whose rewind fails
// propagates the error before any byte moves.
func TestPublishStagedBoundRestreamW24_SeekFailurePropagates(t *testing.T) {
	seekErr := errors.New("w24 restream seek failure")
	err := publishStagedBoundRestream(w24SeekErrFile{err: seekErr}, nil)
	require.ErrorIs(t, err, seekErr)
}

// The virtual/wrapper leg of PublishStagedBound (mem handles carry no
// fd identity) surfaces a CloseStaged *StagingTimesError verbatim — the
// pre-wave-30 times contract rides through the wrapper leg unchanged.
func TestPublishStagedBoundW24_VirtualLegTimesErrorPassthrough(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w24-virtual-times/poster.jpg"
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o644)
	// Removing the staged name makes the post-close virtual Chtimes fail,
	// which CloseStaged wraps as *StagingTimesError.
	require.NoError(t, fs.Remove(staged))

	published := 0
	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(f afero.Fs, src, dst string) error { published++; return PublishNoReplace(f, src, dst) },
		Staged:  staged, Handle: fh, Dest: dest,
		Atime: time.Now(), Mtime: time.Now(), ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr)
	require.NotErrorIs(t, err, ErrPublishStagedClose,
		"a times failure is NOT reclassified as a close failure")
	require.Equal(t, 0, published, "no publish ran after the failed times leg")
}

// w24CloseErrFs wraps MemMapFs and fails Close on staging handles.
type w24CloseErrFs struct {
	afero.Fs
	err error
}

func (f *w24CloseErrFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	fh, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || !strings.Contains(name, ".rstr.") {
		return fh, err
	}
	return &w24CloseErrFile{File: fh, err: f.err}, nil
}

type w24CloseErrFile struct {
	afero.File
	err error
}

func (f *w24CloseErrFile) Close() error {
	_ = f.File.Close()
	return f.err
}

// A mem-handle close failure on the virtual leg wraps as
// ErrPublishStagedClose: nothing was published and the staged name stays put.
func TestPublishStagedBoundW24_VirtualLegCloseErrorWrapsTyped(t *testing.T) {
	closeErr := errors.New("w24 staged close failure")
	inner := afero.NewMemMapFs()
	fs := &w24CloseErrFs{Fs: inner, err: closeErr}
	dest := "/out/w24-virtual-close/poster.jpg"
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o644)

	published := 0
	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(f afero.Fs, src, dst string) error { published++; return PublishNoReplace(f, src, dst) },
		Staged:  staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedClose)
	require.ErrorIs(t, err, closeErr, "the close failure stays unwrap-reachable")
	require.Equal(t, 0, published, "the publish never runs when close fails")
	_, serr := inner.Stat(staged)
	require.NoError(t, serr, "the staged name survives for the caller's cleanup")
}

// ENOSYS from the fd-times seam defers the times onto the PUBLISHED name;
// when THAT Chtimes fails the publish's success is already proven, and the
// failure still surfaces as *StagingTimesError against the destination —
// replayed through publishStagedBoundDeferredChtimes because no portable
// setup fails utimens on a just-published name for root and non-root alike.
func TestPublishStagedBoundW24_DeferredTimesFailureSurfacesTyped(t *testing.T) {
	prevFd := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevFd })

	deferredErr := errors.New("w24 deferred chtimes failure")
	prevDeferred := publishStagedBoundDeferredChtimes
	publishStagedBoundDeferredChtimes = func(afero.Fs, string, time.Time, time.Time) error { return deferredErr }
	t.Cleanup(func() { publishStagedBoundDeferredChtimes = prevDeferred })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Atime: time.Now(), Mtime: time.Now(), ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr)
	require.ErrorIs(t, err, deferredErr)
	require.Equal(t, dest, timesErr.Staged,
		"the deferred failure names the PUBLISHED destination, not the consumed staged name")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the proven publish is never undone by the deferred times failure")
}
