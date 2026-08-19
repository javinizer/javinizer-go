package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w34: codex PR#215 — a transiently wedged final marker unlink must not
// strand a well-formed same-PID marker (a process-lifetime busy block): the
// release path retries with backoff, then rewrites the marker in the
// released form, and any later claimant reclaims it through the takeover
// rules.

func TestReplacementBusyW34_UnlinkWedgeWritesReleasedMarkerAndAcquireReclaims(t *testing.T) {
	setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/out/w34-wedge", 0o755))
	dest := "/out/w34-wedge/poster.jpg"
	path := ReplacementBusyPath(dest)
	removeErr := errors.New("network fs wedged")
	fs := &w34RemoveWedgeFs{Fs: base, path: path, removeErr: removeErr, allowAfter: -1}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	token, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	release()

	require.EqualValues(t, 1+len(replacementBusyReleaseBackoff), fs.attempts.Load(),
		"a persistent wedge must burn the unlink plus every backoff retry")
	content, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	require.Equal(t, replacementBusyReleasedToken(string(token)), string(content))
	require.Contains(t, string(content), replacementBusyReleasedField)
	require.Contains(t, logs.String(), "rewrote it as released")
	require.Contains(t, logs.String(), removeErr.Error())

	// Once the wedge recovers, a fresh claim reclaims the released marker
	// through the takeover rules instead of busy-blocking for this process's
	// lifetime.
	release2, err := AcquireReplacementBusy(base, dest)
	require.NoError(t, err)
	current, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	require.NotEqual(t, content, current)
	require.NotContains(t, string(current), replacementBusyReleasedField)
	release2()
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, w28RecoveryFiles(t, base, "/out/w34-wedge", ".takeover-"))
	require.Empty(t, w28RecoveryFiles(t, base, "/out/w34-wedge", ".quarantine-"))
}

func TestReplacementBusyW34_ReleaseRetryLegs(t *testing.T) {
	t.Run("one backoff leg recovers", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-retry", 0o755))
		dest := "/out/w34-retry/poster.jpg"
		fs := &w34RemoveWedgeFs{Fs: base, path: ReplacementBusyPath(dest), removeErr: errors.New("unlinked transiently"), allowAfter: 1}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		require.EqualValues(t, 2, fs.attempts.Load(), "first unlink fails, first backoff retry succeeds")
		_, err = base.Stat(ReplacementBusyPath(dest))
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Empty(t, logs.String(), "a recovered rename must not warn or leave a released marker")
	})

	t.Run("already-gone marker is a silent no-op", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{0, 0})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-gone", 0o755))
		dest := "/out/w34-gone/poster.jpg"
		path := ReplacementBusyPath(dest)
		fs := &w34RemoveWedgeFs{Fs: base, path: path, removeErr: os.ErrNotExist, allowAfter: -1}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		require.EqualValues(t, 1, fs.attempts.Load(), "a vanished marker ends the retry loop without a backoff")
		current, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, current, "an already-gone unlink must not be rewritten as released")
		require.Empty(t, logs.String())
	})

	t.Run("wedged unlink with foreign rewrite warns and does not clobber", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{0, 0})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-foreign", 0o755))
		dest := "/out/w34-foreign/poster.jpg"
		path := ReplacementBusyPath(dest)
		foreign := []byte(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().UnixNano()))
		fs := &w34RemoveWedgeFs{Fs: base, path: path, removeErr: errors.New("network fs wedged"), allowAfter: -1,
			onRemoveFail: func() { _ = afero.WriteFile(base, path, foreign, 0o600) }}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		current, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, foreign, current, "a marker another writer replaced must survive the wedge")
		require.Contains(t, logs.String(), "no longer match")
	})

	t.Run("wedged unlink and wedged released-rewrite warns manual removal", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{0, 0})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-rewrite", 0o755))
		dest := "/out/w34-rewrite/poster.jpg"
		path := ReplacementBusyPath(dest)
		writeErr := errors.New("released rewrite wedged")
		fs := &w34RemoveWedgeFs{Fs: base, path: path, removeErr: errors.New("network fs wedged"), allowAfter: -1, rewriteErr: writeErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		current, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, current, "a failed released-rewrite must leave the original marker bytes")
		require.Contains(t, logs.String(), "may busy-block until the marker is removed manually")
		require.Contains(t, logs.String(), writeErr.Error())
	})
}

func TestReplacementBusyW34_ReleasedMarkerInspectionArms(t *testing.T) {
	t.Run("released foreign marker is stale and reclaimable", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-inspect", 0o755))
		path := "/out/w34-inspect/poster.jpg.dlbusy"
		raw := replacementBusyReleasedToken(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().UnixNano()))
		require.NoError(t, afero.WriteFile(base, path, []byte(raw), 0o600))
		inspection, err := replacementBusyInspect(base, path)
		require.NoError(t, err)
		require.True(t, inspection.stale)
		require.True(t, inspection.reclaimable)
	})

	// Released field decode: shares parseReplacementBusyToken's field
	// discipline, so lookalikes without pid/time still classify as malformed.
	for _, tc := range []struct {
		content  string
		released bool
	}{
		{"pid=1,time=2,released=1", true},
		{"released=1", true},
		{"pid=1,time=2", false},
		{"pid=1,time=2,released=0", false},
		{"pid=1,time=2,released", false},
		{"junk", false},
	} {
		require.Equal(t, tc.released, replacementBusyIsReleased(tc.content), tc.content)
	}

	t.Run("truly-live same-PID marker still blocks", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-live", 0o755))
		dest := "/out/w34-live/poster.jpg"
		raw := fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
		require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), []byte(raw), 0o600))
		_, err := AcquireReplacementBusy(base, dest)
		require.ErrorIs(t, err, ErrReplacementBusy)
	})

	t.Run("malformed marker with released field is never reclaimed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-malformed", 0o755))
		dest := "/out/w34-malformed/poster.jpg"
		path := ReplacementBusyPath(dest)
		require.NoError(t, afero.WriteFile(base, path, []byte("junk,released=1"), 0o600))
		old := time.Now().Add(-time.Hour)
		require.NoError(t, base.Chtimes(path, old, old))
		_, err := AcquireReplacementBusy(base, dest)
		require.ErrorIs(t, err, ErrReplacementBusy, "released=1 without a well-formed token keeps the never-reclaim rule")
		content, readErr := afero.ReadFile(base, path)
		require.NoError(t, readErr)
		require.Equal(t, "junk,released=1", string(content))
	})
}

func setW34ReleaseBackoff(t *testing.T, backoff []time.Duration) {
	t.Helper()
	old := replacementBusyReleaseBackoff
	replacementBusyReleaseBackoff = backoff
	t.Cleanup(func() { replacementBusyReleaseBackoff = old })
}

// w34RemoveWedgeFs fails the first allowAfter+1 removes of path with
// removeErr (allowAfter < 0 wedges every remove), letting a test wedge a
// transiently or persistently failing final unlink around an otherwise real
// filesystem. onRemoveFail runs on each failed remove, and rewriteErr fails
// the truncating rewrite the released-marker rewrite leg performs.
type w34RemoveWedgeFs struct {
	afero.Fs
	path         string
	removeErr    error
	allowAfter   int32
	rewriteErr   error
	onRemoveFail func()
	attempts     atomic.Int32
}

func (f *w34RemoveWedgeFs) Remove(name string) error {
	if name != f.path {
		return f.Fs.Remove(name)
	}
	attempt := f.attempts.Add(1)
	if f.allowAfter >= 0 && attempt > f.allowAfter {
		return f.Fs.Remove(name)
	}
	if f.onRemoveFail != nil {
		f.onRemoveFail()
	}
	return f.removeErr
}

func (f *w34RemoveWedgeFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.rewriteErr != nil && name == f.path && flag&os.O_TRUNC != 0 {
		return nil, f.rewriteErr
	}
	return f.Fs.OpenFile(name, flag, perm)
}
