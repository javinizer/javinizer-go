package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestAcquireReplacementBusyW28_StaleTokenMismatchRestoresFreePath(t *testing.T) {
	base, dest, fs := newW28Interleave(t, false)
	stale := []byte(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().Add(-time.Hour).UnixNano()))
	require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), stale, 0o600))
	setW28DeadProbe(t)

	bResult := make(chan w28AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		bResult <- w28AcquireResult{release: release, err: err}
	}()
	select {
	case <-fs.firstRenameEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed claimant did not reach takeover rename")
	}

	aResult := make(chan w28AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		aResult <- w28AcquireResult{release: release, err: err}
	}()
	select {
	case <-fs.liveMarkerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("winning claimant did not create its live marker")
	}
	liveToken, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	close(fs.allowFirstRename)

	a := awaitW28Result(t, aResult)
	b := awaitW28Result(t, bResult)
	require.NoError(t, a.err)
	require.NotNil(t, a.release)
	require.ErrorIs(t, b.err, ErrReplacementBusy, "a claimant that renamed a newer live token must back down")
	require.Nil(t, b.release)

	restored, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Equal(t, liveToken, restored, "the delayed claimant must return the marker it took")
	requireW28NoRecoveryArtifacts(t, base, "/out/w28-free")

	a.release()
	_, err = base.Stat(ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW28_StaleTokenMismatchQuarantinesWhenReclaimed(t *testing.T) {
	base, dest, fs := newW28Interleave(t, true)
	stale := []byte(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().Add(-time.Hour).UnixNano()))
	require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), stale, 0o600))
	setW28DeadProbe(t)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	bResult := make(chan w28AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		bResult <- w28AcquireResult{release: release, err: err}
	}()
	select {
	case <-fs.firstRenameEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed claimant did not reach takeover rename")
	}

	aResult := make(chan w28AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		aResult <- w28AcquireResult{release: release, err: err}
	}()
	select {
	case <-fs.liveMarkerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("winning claimant did not create its live marker")
	}
	liveToken, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	a := awaitW28Result(t, aResult)
	require.NoError(t, a.err)
	require.NotNil(t, a.release)
	close(fs.allowFirstRename)

	select {
	case <-fs.secondTakeoverRenamed:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed claimant did not finish the delayed takeover rename")
	}

	thirdResult := make(chan w28AcquireResult, 1)
	go func() {
		release, err := AcquireReplacementBusy(fs, dest)
		thirdResult <- w28AcquireResult{release: release, err: err}
	}()
	third := awaitW28Result(t, thirdResult)
	require.NoError(t, third.err)
	require.NotNil(t, third.release)
	thirdToken, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	// Token byte-distinctness is a POSIX wall-clock property: those clocks
	// advance between the two same-process token mints, so a distinct third
	// token proves the winner's marker was displaced. Windows wall-clock
	// granularity can pin both mints to one tick (Windows CI job 95682090099:
	// pid=7640,time=1787049831878001300 for both), and the marker contract
	// never promised cross-owner entropy within a shared tick. The acquire
	// result above and the checks below still prove ownership transfer there.
	if runtime.GOOS != "windows" {
		require.NotEqual(t, liveToken, thirdToken)
	}
	close(fs.allowSecondTakeoverRename)

	b := awaitW28Result(t, bResult)
	require.ErrorIs(t, b.err, ErrReplacementBusy)
	require.Nil(t, b.release)
	require.Contains(t, logs.String(), "quarantine")

	current, err := afero.ReadFile(base, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Equal(t, thirdToken, current, "the third claimant's live marker must not be overwritten")
	quarantined := w28RecoveryFiles(t, base, "/out/w28-quarantine", ".quarantine-")
	require.Len(t, quarantined, 1)
	preserved, err := afero.ReadFile(base, quarantined[0])
	require.NoError(t, err)
	require.Equal(t, liveToken, preserved, "the marker taken from the winner must survive quarantine")

	a.release()
	if runtime.GOOS != "windows" {
		// POSIX-only for the same shared-tick reason: byte-distinct tokens make
		// the old owner's release a no-match no-op against the third owner's
		// marker. On Windows a colliding token pair makes release legitimately
		// match and remove; both OSes converge on the removal check below once
		// every owner released.
		current, err = afero.ReadFile(base, ReplacementBusyPath(dest))
		require.NoError(t, err)
		require.Equal(t, thirdToken, current, "the old owner's release must not remove the third owner's marker")
	}
	third.release()
	_, err = base.Stat(ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW28_DisappearedInspectionRefreshes(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w28-disappeared/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll("/out/w28-disappeared", 0o755))
	writeW28StaleMarker(t, base, dest)
	setW28DeadProbe(t)

	fs := &w28DisappearOnReadFs{Fs: base, path: path}
	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "a marker that vanished before its bytes were captured must be refreshed")
	release()
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW28_TakeoverReadAndRecoveryErrorsFailClosed(t *testing.T) {
	t.Run("takeover read", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w28-read-error/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w28-read-error", 0o755))
		writeW28StaleMarker(t, base, dest)
		setW28DeadProbe(t)
		readErr := errors.New("takeover read wedged")
		fs := &w28TakeoverReadErrorFs{Fs: base, prefix: ReplacementBusyPath(dest) + ".takeover-", err: readErr}

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, readErr)
	})

	t.Run("mismatch recovery", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w28-recovery-error/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w28-recovery-error", 0o755))
		writeW28StaleMarker(t, base, dest)
		setW28DeadProbe(t)
		reserveErr := errors.New("restore reservation wedged")
		fs := &w28MismatchReserveErrorFs{Fs: base, path: ReplacementBusyPath(dest), reserveErr: reserveErr}

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, reserveErr)
	})
}

func TestReplacementBusyW28_ReturnTakeoverErrorBranches(t *testing.T) {
	t.Run("restore placeholder close", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, false)
		closeErr := errors.New("restore placeholder close wedged")
		fs := &w28FailureFileFs{Fs: base, failPath: path, closeErr: closeErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, closeErr)
	})

	t.Run("restore rename", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, false)
		renameErr := errors.New("restore rename wedged")
		fs := &w28RenameFailureFs{Fs: base, oldPath: takeover, newPath: path, err: renameErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, renameErr)
	})

	t.Run("reserve path", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, false)
		reserveErr := errors.New("reserve path wedged")
		fs := &w28OpenFileFailureFs{Fs: base, failPath: path, err: reserveErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, reserveErr)
	})

	t.Run("quarantine name", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		randomErr := errors.New("quarantine random wedged")
		oldRandom := replacementBusyRandom
		replacementBusyRandom = func() (uint64, error) { return 0, randomErr }
		t.Cleanup(func() { replacementBusyRandom = oldRandom })
		err := replacementBusyReturnTakeover(base, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, randomErr)
	})

	t.Run("quarantine open", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		openErr := errors.New("quarantine open wedged")
		fs := &w28OpenFileFailureFs{Fs: base, failContains: replacementBusyQuarantineMark, err: openErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, openErr)
	})

	t.Run("quarantine write", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		writeErr := errors.New("quarantine write wedged")
		fs := &w28FailureFileFs{Fs: base, failContains: replacementBusyQuarantineMark, writeErr: writeErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, writeErr)
	})

	t.Run("quarantine short write", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		fs := &w28FailureFileFs{Fs: base, failContains: replacementBusyQuarantineMark, shortWrite: true}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.Error(t, err)
		require.Contains(t, err.Error(), "short write")
	})

	t.Run("quarantine sync", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		syncErr := errors.New("quarantine sync wedged")
		fs := &w28FailureFileFs{Fs: base, failContains: replacementBusyQuarantineMark, syncErr: syncErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, syncErr)
	})

	t.Run("quarantine close", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		closeErr := errors.New("quarantine close wedged")
		fs := &w28FailureFileFs{Fs: base, failContains: replacementBusyQuarantineMark, closeErr: closeErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, closeErr)
	})

	t.Run("quarantine remove", func(t *testing.T) {
		base, path, takeover, content := newW28TakeoverFixture(t, true)
		removeErr := errors.New("quarantine remove wedged")
		// Wave-59: releaseClaimedBusyObject delegates to the wave-44 bound
		// unlink, so the terminal remove targets the fresh ".vac." name the
		// vacate rename armed — w59TerminalRemoveFailFs learns that name.
		fs := &w59TerminalRemoveFailFs{Fs: base, err: removeErr, fail: 1}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, removeErr)
	})
}

// w28TakeoverIdentity captures the takeover file's current identity for the
// wave-47 bound takeover-return signature (the restore/quarantine removes
// re-prove the name against THIS).
func w28TakeoverIdentity(t *testing.T, fs afero.Fs, takeover string) os.FileInfo {
	t.Helper()
	info, err := fs.Stat(takeover)
	require.NoError(t, err)
	return info
}

func writeW28StaleMarker(t *testing.T, fs afero.Fs, dest string) {
	t.Helper()
	raw := []byte(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().Add(-time.Hour).UnixNano()))
	require.NoError(t, afero.WriteFile(fs, ReplacementBusyPath(dest), raw, 0o600))
}

func newW28TakeoverFixture(t *testing.T, occupied bool) (afero.Fs, string, string, []byte) {
	t.Helper()
	base := afero.NewMemMapFs()
	dir := "/out/w28-helper"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	path := ReplacementBusyPath(dir + "/poster.jpg")
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	if occupied {
		require.NoError(t, afero.WriteFile(base, path, []byte("pid=999,time=789"), 0o600))
	}
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))
	return base, path, takeover, content
}

type w28DisappearOnReadFs struct {
	afero.Fs
	path string
	seen atomic.Bool
}

func (f *w28DisappearOnReadFs) Open(name string) (afero.File, error) {
	if name == f.path && f.seen.CompareAndSwap(false, true) {
		_ = f.Fs.Remove(name)
		return nil, os.ErrNotExist
	}
	return f.Fs.Open(name)
}

type w28TakeoverReadErrorFs struct {
	afero.Fs
	prefix string
	err    error
}

func (f *w28TakeoverReadErrorFs) Open(name string) (afero.File, error) {
	if strings.HasPrefix(name, f.prefix) {
		return nil, f.err
	}
	return f.Fs.Open(name)
}

type w28MismatchReserveErrorFs struct {
	afero.Fs
	path       string
	reserveErr error
	renamed    atomic.Bool
}

func (f *w28MismatchReserveErrorFs) Rename(oldpath, newpath string) error {
	err := f.Fs.Rename(oldpath, newpath)
	if err == nil && oldpath == f.path {
		f.renamed.Store(true)
		_ = afero.WriteFile(f.Fs, newpath, []byte("pid=123,time=789"), 0o600)
	}
	return err
}

func (f *w28MismatchReserveErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.path && f.renamed.Load() && flag&os.O_EXCL != 0 {
		return nil, f.reserveErr
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w28OpenFileFailureFs struct {
	afero.Fs
	failPath     string
	failContains string
	err          error
}

func (f *w28OpenFileFailureFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if (f.failPath != "" && name == f.failPath) || (f.failContains != "" && strings.Contains(name, f.failContains)) {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w28RenameFailureFs struct {
	afero.Fs
	oldPath string
	newPath string
	err     error
}

func (f *w28RenameFailureFs) Rename(oldpath, newpath string) error {
	if oldpath == f.oldPath && newpath == f.newPath {
		return f.err
	}
	return f.Fs.Rename(oldpath, newpath)
}

type w28FailureFileFs struct {
	afero.Fs
	failPath     string
	failContains string
	writeErr     error
	shortWrite   bool
	syncErr      error
	closeErr     error
}

func (f *w28FailureFileFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if (f.failPath != "" && name == f.failPath) || (f.failContains != "" && strings.Contains(name, f.failContains)) {
		return &w28FailureFile{File: file, writeErr: f.writeErr, shortWrite: f.shortWrite, syncErr: f.syncErr, closeErr: f.closeErr}, nil
	}
	return file, nil
}

type w28FailureFile struct {
	afero.File
	writeErr   error
	shortWrite bool
	syncErr    error
	closeErr   error
}

func (f *w28FailureFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.shortWrite {
		return len(p) - 1, nil
	}
	return f.File.Write(p)
}

func (f *w28FailureFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

func (f *w28FailureFile) Close() error {
	err := f.File.Close()
	if f.closeErr != nil {
		return f.closeErr
	}
	return err
}

func setW28DeadProbe(t *testing.T) {
	t.Helper()
	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})
}

type w28AcquireResult struct {
	release func()
	err     error
}

type w28InterleaveFs struct {
	afero.Fs
	markerPath                string
	blockSecondTakeoverRead   bool
	firstRenameEntered        chan struct{}
	allowFirstRename          chan struct{}
	liveMarkerReady           chan struct{}
	secondTakeoverRenamed     chan struct{}
	allowSecondTakeoverRename chan struct{}
	firstRenameOnce           sync.Once
	liveMarkerOnce            sync.Once
	secondRenameOnce          sync.Once
	renameCalls               atomic.Int32
	markerCreates             atomic.Int32
}

func newW28Interleave(t *testing.T, blockSecondRead bool) (afero.Fs, string, *w28InterleaveFs) {
	t.Helper()
	base := afero.NewMemMapFs()
	dir := "/out/w28-free"
	if blockSecondRead {
		dir = "/out/w28-quarantine"
	}
	require.NoError(t, base.MkdirAll(dir, 0o755))
	dest := dir + "/poster.jpg"
	return base, dest, &w28InterleaveFs{
		Fs:                        base,
		markerPath:                ReplacementBusyPath(dest),
		blockSecondTakeoverRead:   blockSecondRead,
		firstRenameEntered:        make(chan struct{}),
		allowFirstRename:          make(chan struct{}),
		liveMarkerReady:           make(chan struct{}),
		secondTakeoverRenamed:     make(chan struct{}),
		allowSecondTakeoverRename: make(chan struct{}),
	}
}

func (f *w28InterleaveFs) Rename(oldpath, newpath string) error {
	call := f.renameCalls.Add(1)
	if call == 1 {
		f.firstRenameOnce.Do(func() { close(f.firstRenameEntered) })
		<-f.allowFirstRename
		err := f.Fs.Rename(oldpath, newpath)
		if f.blockSecondTakeoverRead {
			f.secondRenameOnce.Do(func() { close(f.secondTakeoverRenamed) })
			<-f.allowSecondTakeoverRename
		}
		return err
	}
	return f.Fs.Rename(oldpath, newpath)
}

func (f *w28InterleaveFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if name == f.markerPath && flag&os.O_EXCL != 0 && f.markerCreates.Add(1) == 1 {
		return &w28CloseSignalFile{
			File:   file,
			signal: func() { f.liveMarkerOnce.Do(func() { close(f.liveMarkerReady) }) },
		}, nil
	}
	return file, nil
}

type w28CloseSignalFile struct {
	afero.File
	signal func()
}

func (f *w28CloseSignalFile) Close() error {
	err := f.File.Close()
	if err == nil {
		f.signal()
	}
	return err
}

func awaitW28Result(t *testing.T, ch <-chan w28AcquireResult) w28AcquireResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("claimant did not finish")
		return w28AcquireResult{}
	}
}

func requireW28NoRecoveryArtifacts(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	for _, entry := range w28RecoveryFiles(t, fs, dir, ".takeover-") {
		t.Errorf("unexpected takeover artifact %s", entry)
	}
	for _, entry := range w28RecoveryFiles(t, fs, dir, ".quarantine-") {
		t.Errorf("unexpected quarantine artifact %s", entry)
	}
}

func w28RecoveryFiles(t *testing.T, fs afero.Fs, dir, marker string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	var paths []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), marker) {
			paths = append(paths, dir+"/"+entry.Name())
		}
	}
	return paths
}
