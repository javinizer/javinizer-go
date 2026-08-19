//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-29 (P1), POSIX legs — the
// HANDLE-seam recordings: the mode re-assert at create and the times
// application inside CloseStaged must run fd-scoped through the open staging
// handle on the real OsFs, never by path. The mechanism is pinned by the
// recording seams (stagedHandleChmod / stagedHandleChtimes) and by outcome:
// a restrictive umask narrows O_CREATE modes, so only an authoritative
// through-handle re-assert lands the requested bits exactly.

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

// w29ChmodRecord records one stagedHandleChmod invocation.
type w29ChmodRecord struct {
	fd   uintptr
	mode os.FileMode
}

func TestCreateExclusiveStagingFileW29_ModeAssertedThroughHandleSeam(t *testing.T) {
	// NOT parallel: syscall.Umask is process-wide for the test window.
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	var records []w29ChmodRecord
	prev := stagedHandleChmod
	stagedHandleChmod = func(f *os.File, mode os.FileMode) error {
		records = append(records, w29ChmodRecord{fd: f.Fd(), mode: mode})
		return prev(f, mode)
	}
	t.Cleanup(func() { stagedHandleChmod = prev })

	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o666)
	require.NoError(t, err)
	defer func() { _ = fh.Close() }()
	oh, ok := fh.(*os.File)
	require.True(t, ok, "the OsFs staging handle is an *os.File")

	require.Len(t, records, 1, "exactly one through-handle mode re-assert")
	require.Equal(t, oh.Fd(), records[0].fd, "the chmod rode the open staging handle's descriptor")
	require.Equal(t, os.FileMode(0o666), records[0].mode)

	info, err := os.Stat(staged)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o666), info.Mode().Perm(),
		"umask 0077 narrowed O_CREATE to 0600; only an authoritative through-handle chmod lands 0666")
}

// w29TimesRecord records one stagedHandleChtimes invocation.
type w29TimesRecord struct {
	fd           uintptr
	atime, mtime time.Time
}

func TestCloseStagedW29_TimesAppliedThroughHandleSeam(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	var records []w29TimesRecord
	prev := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
		records = append(records, w29TimesRecord{fd: fd, atime: atime, mtime: mtime})
		return prev(fd, atime, mtime)
	}
	t.Cleanup(func() { stagedHandleChtimes = prev })

	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)
	oh, ok := fh.(*os.File)
	require.True(t, ok, "the OsFs staging handle is an *os.File")
	openFD := oh.Fd()

	ancient := time.Unix(946684800, 0)
	require.NoError(t, CloseStaged(fs, staged, fh, ancient, ancient, true))

	require.Len(t, records, 1, "exactly one through-handle times application")
	require.Equal(t, openFD, records[0].fd, "the times rode the open staging handle's descriptor, never the staged path")
	require.True(t, records[0].mtime.Equal(ancient))

	info, err := os.Stat(staged)
	require.NoError(t, err)
	require.Equal(t, ancient.Unix(), info.ModTime().Unix(),
		"the published-ready inode carries the requested mtime")
}

// A handle-times failure closes the handle before surfacing the TYPED
// StagingTimesError (the inode stays staged for the caller's remove leg).
func TestCloseStagedW29_HandleTimesFailureClosesAndTypes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	wedge := errors.New("w29 handle times wedged")
	prev := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return wedge }
	t.Cleanup(func() { stagedHandleChtimes = prev })

	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged"))
	require.NoError(t, err)

	err = CloseStaged(fs, staged, fh, time.Unix(946684800, 0), time.Unix(946684800, 0), true)
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr)
	require.ErrorIs(t, err, wedge)
	require.Error(t, fh.Close(), "the typed failure already closed the staging handle")

	_, statErr := os.Stat(staged)
	require.NoError(t, statErr, "the inode remains staged for the caller's cleanup decision")
}

// The ownership hand-off rides the SAME handle: RestoreStagingOwnership is
// covered by replacements_cov_w7_test.go's fchown seam recordings; this pin
// proves production wiring kept the OsFs legs consistent: a fresh handle's
// ownership hand-off + times + identity proof + publish compose end-to-end.
func TestExclusiveStagingW29_EndToEndHandleTailPublishes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	source := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(source, []byte("restored"), 0o600))
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)

	dest := filepath.Join(dir, "poster.jpg")
	staged, fh, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 9, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("restored"))
	require.NoError(t, err)

	RestoreStagingOwnership(fs, fh, sourceInfo)
	require.NoError(t, VerifyStagedIdentity(fs, staged, fh))

	ancient := time.Unix(946684800, 0)
	require.NoError(t, CloseStaged(fs, staged, fh, ancient, ancient, true))
	require.NoError(t, PublishNoReplace(fs, staged, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "restored", string(got))
	info, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	require.Equal(t, ancient.Unix(), info.ModTime().Unix())
	_, err = os.Stat(staged)
	require.ErrorIs(t, err, os.ErrNotExist, "the publish consumed the staged name")
}
