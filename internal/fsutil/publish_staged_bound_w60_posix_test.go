//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-60 (P2) — the ENOSYS deferred-
// times leg (AIX/Solaris: stagedHandleChtimes answers ENOSYS, so the times
// land by name on the PUBLISHED destination AFTER the publish+reverify
// already succeeded). Pre-wave-60 a Chtimes failure there returned a plain
// *StagingTimesError, so callers treated a completed, identity-verified
// publish as a pre-publish staging failure: they retained the backup, left
// the journal entry unconsumed, and reported a misleading failed create —
// when in fact the destination provably carries the published bytes.
//
// wave-60 re-derives the destination identity at the error step:
//
//   - (a) the destination STILL names the staged inode → the times error is
//     joined with ErrPublishCompleted so callers run their established
//     completed-publish discipline (wave-34: journal confirm + backup
//     consumed, pending-kind Clean);
//   - (b) the identity drifted (foreign occupant / vanished / indeterminate)
//     → the published bytes are no longer provably at dest, so the plain
//     *StagingTimesError stays (retained backup, unconfirmed journal).

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

// w60DeferredCase forces the ENOSYS deferred-times leg (fd times answer
// ENOSYS) and wedges the name-based deferred Chtimes, staging a genuine
// file plus a foreign neighbour whose identity the divergent leg swaps in.
func w60DeferredCase(t *testing.T) (dest, staged string, fh afero.File, foreignInfo os.FileInfo, deferredErr error) {
	t.Helper()
	prevFd := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevFd })

	deferredErr = errors.New("w60 deferred chtimes failure")
	prevDeferred := publishStagedBoundDeferredChtimes
	publishStagedBoundDeferredChtimes = func(afero.Fs, string, time.Time, time.Time) error { return deferredErr }
	t.Cleanup(func() { publishStagedBoundDeferredChtimes = prevDeferred })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest = filepath.Join(dir, "poster.jpg")
	staged, fh = w30Stage(t, fs, dest, ".rstr", 0o640)
	foreign := filepath.Join(dir, "foreign.jpg")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign replacement"), 0o644))
	var err error
	foreignInfo, err = os.Lstat(foreign)
	require.NoError(t, err)
	return dest, staged, fh, foreignInfo, deferredErr
}

// w60Run drives the destination-lookup seam by call number for the divergent
// leg: 1 = post-publish reverify (must name our inode), 2 = pre-Chtimes
// ownership re-proof (must name our inode), 3 = the wave-60 error-step
// identity re-derivation.
func w60Run(t *testing.T, dest, staged string, fh afero.File, onThird func(calls int, name string) (os.FileInfo, error)) error {
	t.Helper()
	calls := 0
	prevLstat := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		calls++
		if calls == 3 {
			return onThird(calls, name)
		}
		return os.Lstat(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLstat })
	_, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: time.Now(), Mtime: time.Now(),
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	return err
}

// (a) verified destination: a deferred-times failure on a dest that STILL
// names the staged inode surfaces ErrPublishCompleted (NOT a plain
// *StagingTimesError) — the publish already landed, so callers run their
// completed-publish discipline. The destination keeps the published bytes.
func TestPublishStagedBoundW60POSIX_DeferredTimesFailureVerifiedSurfacesCompleted(t *testing.T) {
	dest, staged, fh, _, deferredErr := w60DeferredCase(t)
	// No lstat seam: real os.Lstat names our published inode at every
	// lookup, including the wave-60 error-step re-derivation.
	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: time.Now(), Mtime: time.Now(),
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCompleted, "a verified publish with a deferred-times failure is a completed publish")
	require.ErrorIs(t, err, deferredErr, "the underlying times error stays unwrap-reachable")
	var timesErr *StagingTimesError
	require.NotErrorIs(t, err, ErrPublishStagedIdentityBreak, "the publish was verified — no identity break")
	require.False(t, errors.As(err, &timesErr),
		"the verified leg is NOT a *StagingTimesError — callers must reach the completed-publish (default) arm, not the times arm")
	require.Nil(t, info, "no identity is handed back on the times-failure refusal")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the proven publish is never undone by the deferred times failure")
}

// (b) divergent identity: the error-step re-derivation names a DIFFERENT
// inode — the published bytes are no longer provably at dest, so the plain
// *StagingTimesError stays (retained backup, unconfirmed journal), never
// ErrPublishCompleted.
func TestPublishStagedBoundW60POSIX_DeferredTimesFailureDivergentSurfacesTyped(t *testing.T) {
	dest, staged, fh, foreignInfo, deferredErr := w60DeferredCase(t)
	err := w60Run(t, dest, staged, fh, func(int, string) (os.FileInfo, error) {
		return foreignInfo, nil // dest no longer names the staged inode
	})
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr, "a divergent deferred-times failure keeps the typed staging-times class")
	require.Equal(t, dest, timesErr.Staged,
		"the deferred failure names the PUBLISHED destination, not the consumed staged name")
	require.ErrorIs(t, err, deferredErr)
	require.NotErrorIs(t, err, ErrPublishCompleted,
		"identity drift means the publish is no longer provably at dest — never the completed class")
}
