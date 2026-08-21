package fsutil

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestAcquireReplacementBusyW20A_AtomicTakeoverClaim(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-interleave/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-interleave", 0o755))
	writeW20BusyToken(t, base, dest, 999999999, time.Now().Add(-time.Hour))

	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})

	fs := newW20InterleaveFs(base, path)
	firstResult := make(chan w20AcquireResult, 1)
	secondResult := make(chan w20AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		firstResult <- w20AcquireResult{release: release, err: err}
	}()
	select {
	case <-fs.firstRenameEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first claimant did not reach takeover rename")
	}

	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		secondResult <- w20AcquireResult{release: release, err: err}
	}()
	first := awaitW20Result(t, firstResult)
	second := awaitW20Result(t, secondResult)

	require.NoError(t, first.err, "the first source rename claimant owns cleanup")
	require.NotNil(t, first.release)
	require.ErrorIs(t, second.err, ErrReplacementBusy, "the rename loser must re-read the new live marker")
	require.Nil(t, second.release)
	require.Equal(t, int32(3), fs.renameCalls.Load(), "two platform takeover renames + the wave-59 bound-unlink vacate rename of the reclaimed takeover")

	content, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	require.Contains(t, string(content), "pid=")
	entries, err := afero.ReadDir(base, "/out/w20-interleave")
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), ".takeover-", "a losing claimant must not leave a successor")
	}

	first.release()
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW20B_MalformedStaleMarkerIsNeverTakenOver(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-malformed/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-malformed", 0o755))
	require.NoError(t, afero.WriteFile(base, path, []byte("user-owned bytes"), 0o644))
	old := time.Now().Add(-replacementBusyStaleAge - time.Minute)
	require.NoError(t, base.Chtimes(path, old, old))

	fs := &w20RenameCountFs{Fs: base}
	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy)
	require.Equal(t, int32(0), fs.renameCalls.Load(), "malformed content is not eligible for takeover")
	info, err := base.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	content, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	require.Equal(t, "user-owned bytes", string(content))
}

func TestAcquireReplacementBusyW20C_PIDReuseStartTimeArms(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-pid-reuse/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/w20-pid-reuse", 0o755))
	pid := os.Getpid() + 10000
	markerTime := time.Now().Add(-time.Second)
	writeW20BusyToken(t, base, dest, pid, markerTime)

	oldProbe := replacementProbePIDAliveAware
	oldStart := replacementProcessStartTime
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(gotPID int) replacementPIDLiveness {
		require.Equal(t, pid, gotPID)
		return replacementPIDAlive
	}
	replacementProcessStartTime = func(gotPID int) *time.Time {
		require.Equal(t, pid, gotPID)
		start := markerTime.Add(time.Second)
		return &start
	}
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementProcessStartTime = oldStart
		replacementIsWindows = oldWindows
	})

	release, err := AcquireReplacementBusy(base, dest)
	require.NoError(t, err, "a live PID whose start is after the marker is a reused PID")
	release()

	liveMarkerTime := time.Now()
	writeW20BusyToken(t, base, dest, pid, liveMarkerTime)
	replacementProcessStartTime = func(gotPID int) *time.Time {
		require.Equal(t, pid, gotPID)
		start := liveMarkerTime.Add(-time.Second)
		return &start
	}
	before, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	_, err = AcquireReplacementBusy(base, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a live PID whose start predates the marker is the owner")
	after, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// W20D is retained as a regression test, but its old POSIX age expectation is
// deliberately adapted for W23: a positive liveness result remains busy when
// start time is unavailable; only an undecidable probe may use marker age.
func TestAcquireReplacementBusyW20D_LivenessPrecedesAgeAndUndecidableFallsBack(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-fallback/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-fallback", 0o755))
	pid := os.Getpid() + 10001

	oldProbe := replacementProbePIDAliveAware
	oldStart := replacementProcessStartTime
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDAlive }
	replacementProcessStartTime = func(int) *time.Time { return nil }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementProcessStartTime = oldStart
		replacementIsWindows = oldWindows
	})

	writeW20BusyToken(t, base, dest, pid, time.Now().Add(-replacementBusyStaleAge-time.Second))
	_, err := AcquireReplacementBusy(base, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a live POSIX marker stays busy past the old age threshold")

	require.NoError(t, base.Remove(path))
	// The injectable probe models macOS/other non-Linux POSIX when liveness is
	// undecidable; this is the only arm that may fall back to marker age.
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDUnprobeable }
	writeW20BusyToken(t, base, dest, pid, time.Now().Add(-replacementBusyStaleAge-time.Second))
	release, err := AcquireReplacementBusy(base, dest)
	require.NoError(t, err, "an undecidable POSIX probe uses the stale-age fallback")
	release()

	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDUnprobeable }
	writeW20BusyToken(t, base, dest, pid, time.Now())
	_, err = AcquireReplacementBusy(base, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a fresh undecidable marker remains busy")

	require.NoError(t, base.Remove(path))
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDLiveness(99) }
	writeW20BusyToken(t, base, dest, pid, time.Now().Add(-replacementBusyStaleAge-time.Second))
	_, err = AcquireReplacementBusy(base, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "an unknown probe result fails closed")
}

func TestAcquireReplacementBusyW20E_ClaimFailuresFailClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-errors/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-errors", 0o755))
	pid := os.Getpid() + 10002
	oldProbe := replacementProbePIDAliveAware
	oldStart := replacementProcessStartTime
	oldWindows := replacementIsWindows
	oldRandom := replacementBusyRandom
	oldCryptoRead := replacementCryptoRandomRead
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementProcessStartTime = oldStart
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementProcessStartTime = oldStart
		replacementIsWindows = oldWindows
		replacementBusyRandom = oldRandom
		replacementCryptoRandomRead = oldCryptoRead
	})

	randomErr := errors.New("random wedged")
	replacementCryptoRandomRead = func([]byte) (int, error) { return 0, randomErr }
	_, err := replacementBusyRandomPlatform()
	require.ErrorIs(t, err, randomErr)
	replacementCryptoRandomRead = oldCryptoRead

	writeW20BusyToken(t, base, dest, pid, time.Now().Add(-time.Hour))
	replacementBusyRandom = func() (uint64, error) { return 0, randomErr }
	_, err = AcquireReplacementBusy(base, dest)
	require.ErrorIs(t, err, randomErr)
	_, err = base.Stat(path)
	require.NoError(t, err, "name-generation failure leaves the old marker in place")

	require.NoError(t, base.Remove(path))
	writeW20BusyToken(t, base, dest, pid, time.Now().Add(-time.Hour))
	replacementBusyRandom = func() (uint64, error) { return 1, nil }
	renameErr := errors.New("rename wedged")
	fs := &w20RenameCountFs{Fs: base, renameErr: renameErr}
	_, err = AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, renameErr)
	_, err = base.Stat(path)
	require.NoError(t, err, "rename failure leaves the old marker in place")
}

func TestAcquireReplacementBusyW20G_LoserReinspectionRetainsMalformedMarker(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-lost-malformed/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-lost-malformed", 0o755))
	writeW20BusyToken(t, base, dest, 999999999, time.Now().Add(-time.Hour))

	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})

	old := time.Now().Add(-replacementBusyStaleAge - time.Minute)
	fs := &w20LostMalformedFs{Fs: base, path: path, marker: []byte("user-owned bytes"), markerTime: old}
	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy)
	require.Equal(t, int32(1), fs.renameCalls.Load())
	content, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	require.Equal(t, "user-owned bytes", string(content))
}

func TestAcquireReplacementBusyW20H_LostSourceIsRecheckedBeforeCreate(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-lost-absent/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-lost-absent", 0o755))
	writeW20BusyToken(t, base, dest, 999999999, time.Now().Add(-time.Hour))

	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})

	fs := &w20VanishedTakeoverFs{Fs: base}
	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	require.Equal(t, int32(1), fs.renameCalls.Load())
	release()
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW20F_LoserReinspectionErrorsFailClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w20-reinspect/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w20-reinspect", 0o755))
	writeW20BusyToken(t, base, dest, 999999999, time.Now().Add(-time.Hour))

	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})

	inspectErr := errors.New("reinspection wedged")
	fs := &w20LostTakeoverFs{Fs: base, path: path, inspectErr: inspectErr}
	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, inspectErr)
	require.Equal(t, int32(1), fs.renameCalls.Load())
}

func writeW20BusyToken(t *testing.T, fs afero.Fs, dest string, pid int, created time.Time) {
	t.Helper()
	raw := []byte(fmt.Sprintf("pid=%d,time=%d", pid, created.UnixNano()))
	require.NoError(t, afero.WriteFile(fs, ReplacementBusyPath(dest), raw, 0o600))
}

type w20AcquireResult struct {
	release func()
	err     error
}

func awaitW20Result(t *testing.T, ch <-chan w20AcquireResult) w20AcquireResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("claimant did not finish")
		return w20AcquireResult{}
	}
}

type w20InterleaveFs struct {
	afero.Fs
	path                string
	firstRenameEntered  chan struct{}
	secondRenameEntered chan struct{}
	firstRenamed        chan struct{}
	secondAttempted     chan struct{}
	markerRecreated     chan struct{}
	firstEnteredOnce    sync.Once
	secondEnteredOnce   sync.Once
	firstRenamedOnce    sync.Once
	secondAttemptedOnce sync.Once
	markerRecreatedOnce sync.Once
	renameCalls         atomic.Int32
}

func newW20InterleaveFs(fs afero.Fs, path string) *w20InterleaveFs {
	return &w20InterleaveFs{
		Fs:                  fs,
		path:                path,
		firstRenameEntered:  make(chan struct{}),
		secondRenameEntered: make(chan struct{}),
		firstRenamed:        make(chan struct{}),
		secondAttempted:     make(chan struct{}),
		markerRecreated:     make(chan struct{}),
	}
}

func (f *w20InterleaveFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && name == f.path && flag&os.O_EXCL != 0 {
		f.markerRecreatedOnce.Do(func() { close(f.markerRecreated) })
	}
	return file, err
}

func (f *w20InterleaveFs) Rename(oldpath, newpath string) error {
	call := f.renameCalls.Add(1)
	switch call {
	case 1:
		f.firstEnteredOnce.Do(func() { close(f.firstRenameEntered) })
		<-f.secondRenameEntered
		err := f.Fs.Rename(oldpath, newpath)
		f.firstRenamedOnce.Do(func() { close(f.firstRenamed) })
		<-f.secondAttempted
		return err
	case 2:
		f.secondEnteredOnce.Do(func() { close(f.secondRenameEntered) })
		<-f.firstRenamed
		err := f.Fs.Rename(oldpath, newpath)
		f.secondAttemptedOnce.Do(func() { close(f.secondAttempted) })
		<-f.markerRecreated
		return err
	default:
		return f.Fs.Rename(oldpath, newpath)
	}
}

type w20RenameCountFs struct {
	afero.Fs
	renameErr   error
	renameCalls atomic.Int32
}

func (f *w20RenameCountFs) Rename(oldpath, newpath string) error {
	f.renameCalls.Add(1)
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.Fs.Rename(oldpath, newpath)
}

type w20LostTakeoverFs struct {
	afero.Fs
	path        string
	inspectErr  error
	renameCalls atomic.Int32
}

type w20LostMalformedFs struct {
	afero.Fs
	path        string
	marker      []byte
	markerTime  time.Time
	renameCalls atomic.Int32
}

type w20VanishedTakeoverFs struct {
	afero.Fs
	renameCalls atomic.Int32
}

func (f *w20LostTakeoverFs) Rename(oldpath, newpath string) error {
	f.renameCalls.Add(1)
	return os.ErrNotExist
}

func (f *w20LostMalformedFs) Rename(oldpath, newpath string) error {
	f.renameCalls.Add(1)
	_ = afero.WriteFile(f.Fs, f.path, f.marker, 0o644)
	_ = f.Fs.Chtimes(f.path, f.markerTime, f.markerTime)
	return os.ErrNotExist
}

func (f *w20VanishedTakeoverFs) Rename(oldpath, newpath string) error {
	// The vanish replays ONLY at acquire's reclaim rename (the first rename
	// of the flow); wave-43's take-aside renames inside the later release
	// behave normally.
	if f.renameCalls.Add(1) == 1 {
		if err := f.Fs.Remove(oldpath); err != nil {
			return err
		}
		return os.ErrNotExist
	}
	return f.Fs.Rename(oldpath, newpath)
}

func (f *w20LostTakeoverFs) Open(name string) (afero.File, error) {
	if f.renameCalls.Load() > 0 && strings.HasSuffix(name, ReplacementBusySuffix) {
		return nil, f.inspectErr
	}
	return f.Fs.Open(name)
}
