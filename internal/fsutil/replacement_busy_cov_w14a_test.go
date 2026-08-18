package fsutil

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementBusyW14A_LifecycleAndReclamation(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w14a/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w14a", 0o755))

	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	content, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Contains(t, string(content), "pid=")
	_, err = AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy)
	release()
	release()
	_, err = fs.Stat(ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)

	deadMarkerTime := time.Now()
	if runtime.GOOS == "windows" {
		// Windows uses the marker timestamp rather than probing a PID.
		deadMarkerTime = deadMarkerTime.Add(-time.Hour)
	}
	writeW14ABusyToken(t, fs, dest, 999999999, deadMarkerTime)
	release, err = AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "old foreign markers are reclaimable")
	release()

	writeW14ABusyToken(t, fs, dest, os.Getpid(), time.Now().Add(-time.Hour))
	release, err = AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "same-PID markers from before this boot are stale")
	release()

	if runtime.GOOS == "windows" {
		// Windows ownership is timestamp-driven for foreign PIDs; a recent
		// marker remains busy without probing the PID.
		writeW14ABusyToken(t, fs, dest, 999999999, time.Now())
		stale, err := replacementBusyStale(fs, ReplacementBusyPath(dest))
		require.NoError(t, err)
		require.False(t, stale, "a recent foreign marker is live on Windows")
		require.NoError(t, fs.Remove(ReplacementBusyPath(dest)))
	} else {
		require.True(t, replacementProcessAlive(os.Getpid()))
	}
	require.False(t, replacementProcessAlive(0))
	findProcess := replacementFindProcess
	replacementFindProcess = func(int) (*os.Process, error) { return nil, errors.New("find process wedged") }
	require.False(t, replacementProcessAlive(123))
	replacementFindProcess = findProcess
	require.True(t, ReplacementBusyPath(dest) != dest)
}

func TestReplacementBusyW14A_MalformedFreshAndStale(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w14a-malformed/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w14a-malformed", 0o755))
	path := ReplacementBusyPath(dest)
	require.NoError(t, afero.WriteFile(fs, path, []byte("partial"), 0o600))

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a fresh partial marker is treated as live")
	stale, err := replacementBusyStale(fs, "/out/w14a-malformed/missing.dlbusy")
	require.NoError(t, err)
	require.True(t, stale)
	stale, err = replacementBusyStale(&w14AStatNotExistFs{Fs: fs}, path)
	require.NoError(t, err)
	require.True(t, stale)

	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(path, old, old))
	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "an old malformed marker is eventually reclaimable")
	release()

	foreign := "/out/w14a-malformed/foreign.jpg"
	isWindows := replacementIsWindows
	replacementIsWindows = true
	t.Cleanup(func() { replacementIsWindows = isWindows })
	writeW14ABusyToken(t, fs, foreign, 999999999, time.Now())
	stale, err = replacementBusyStale(fs, ReplacementBusyPath(foreign))
	require.NoError(t, err)
	require.False(t, stale, "a recent foreign marker is live under Windows timestamp policy")
	require.NoError(t, fs.Remove(ReplacementBusyPath(foreign)))
	writeW14ABusyToken(t, fs, foreign, 999999999, time.Now().Add(-time.Hour))
	stale, err = replacementBusyStale(fs, ReplacementBusyPath(foreign))
	require.NoError(t, err)
	require.True(t, stale)

	for _, raw := range []string{"", "pid=x,time=1", "pid=1,time=x", "pid=1", "time=1", "junk"} {
		_, _, ok := parseReplacementBusyToken(raw)
		require.False(t, ok, raw)
	}
}

func TestReplacementBusyW14A_ArbitrationErrors(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w14a-errors/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/w14a-errors", 0o755))

	_, err := AcquireReplacementBusy(afero.NewReadOnlyFs(base), dest)
	require.Error(t, err)

	for _, tc := range []struct {
		name string
		file w14AFileFailure
	}{
		{name: "write", file: w14AFileFailure{writeErr: errors.New("write wedged")}},
		{name: "sync", file: w14AFileFailure{syncErr: errors.New("sync wedged")}},
		{name: "close", file: w14AFileFailure{closeErr: errors.New("close wedged")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &w14AFileFailureFs{Fs: afero.NewMemMapFs(), failure: tc.file}
			require.NoError(t, fs.MkdirAll("/out/w14a-errors", 0o755))
			_, err := AcquireReplacementBusy(fs, dest)
			require.Error(t, err)
		})
	}

	readErr := errors.New("marker read wedged")
	require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), []byte("pid=999999999,time=1"), 0o600))
	_, err = AcquireReplacementBusy(&w14AReadFailureFs{Fs: base, err: readErr}, dest)
	require.ErrorIs(t, err, readErr)

	statErr := errors.New("marker stat wedged")
	require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), []byte("pid=999999999,time=1"), 0o600))
	_, err = AcquireReplacementBusy(&w14AStatFailureFs{Fs: base, err: statErr}, dest)
	require.ErrorIs(t, err, statErr)

	removeErr := errors.New("marker remove wedged")
	require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), []byte("pid=999999999,time=1"), 0o600))
	_, err = AcquireReplacementBusy(&w14ARemoveFailureFs{Fs: base, err: removeErr}, dest)
	require.ErrorIs(t, err, removeErr)

	releaseReplacementBusy(base, ReplacementBusyPath(dest), "not-the-owner")
}

func writeW14ABusyToken(t *testing.T, fs afero.Fs, dest string, pid int, created time.Time) {
	t.Helper()
	raw := []byte("pid=" + strconv.Itoa(pid) + ",time=" + strconv.FormatInt(created.UnixNano(), 10))
	require.NoError(t, afero.WriteFile(fs, ReplacementBusyPath(dest), raw, 0o600))
}

type w14AFileFailure struct {
	writeErr error
	syncErr  error
	closeErr error
}

type w14AFileFailureFs struct {
	afero.Fs
	failure w14AFileFailure
}

func (f *w14AFileFailureFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &w14AFileFailureFile{File: file, failure: f.failure}, nil
}

type w14AFileFailureFile struct {
	afero.File
	failure w14AFileFailure
}

func (f *w14AFileFailureFile) WriteString(string) (int, error) {
	if f.failure.writeErr != nil {
		return 0, f.failure.writeErr
	}
	return f.File.WriteString("ok")
}

func (f *w14AFileFailureFile) Sync() error {
	if f.failure.syncErr != nil {
		return f.failure.syncErr
	}
	return f.File.Sync()
}

func (f *w14AFileFailureFile) Close() error {
	err := f.File.Close()
	if f.failure.closeErr != nil {
		return f.failure.closeErr
	}
	return err
}

type w14AReadFailureFs struct {
	afero.Fs
	err error
}

func (f *w14AReadFailureFs) Open(name string) (afero.File, error) {
	if strings.HasSuffix(name, ReplacementBusySuffix) {
		return nil, f.err
	}
	return f.Fs.Open(name)
}

type w14AStatFailureFs struct {
	afero.Fs
	err error
}

func (f *w14AStatFailureFs) Stat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, ReplacementBusySuffix) {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

type w14ARemoveFailureFs struct {
	afero.Fs
	err error
}

func (f *w14ARemoveFailureFs) Remove(name string) error {
	if strings.HasSuffix(name, ReplacementBusySuffix) {
		return f.err
	}
	return f.Fs.Remove(name)
}

type w14AStatNotExistFs struct {
	afero.Fs
}

func (f *w14AStatNotExistFs) Stat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, ReplacementBusySuffix) {
		return nil, os.ErrNotExist
	}
	return f.Fs.Stat(name)
}
