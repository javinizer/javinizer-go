package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-17 (P2) — "remove the takeover
// placeholder when close fails": the close-error branch of
// replacementBusyReturnTakeover used to return with its zero-byte
// O_CREATE|O_EXCL placeholder still occupying the marker path — a tokenless
// (malformed) marker that is deliberately NEVER reclaimable, permanently
// busy-blocking the destination. The branch now recovers in the order that
// can never strand BOTH the displaced bytes and an unreclaimable marker:
// restore the takeover content back over the placeholder first (keeping the
// placeholder's serialization through the restore), and only on restore
// failure remove the placeholder so the marker self-heals.

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w17CloseFailFile fails Close with a fixed error after delegating to the
// real close (the placeholder IS created; only its close reports failure).
type w17CloseFailFile struct {
	afero.File
	err error
}

func (f *w17CloseFailFile) Close() error {
	_ = f.File.Close()
	return f.err
}

// w17PlaceholderWedgeFs wedges the takeover-return close path: Close failure
// on the marker placeholder, an optional restore (rename) failure for the
// takeover→path leg, and an optional terminal-remove failure (wave-59: the
// bound unlink removes the placeholder via a fresh ".vac." terminal name, so
// the wedge arms that name through the vacate rename and fails its remove).
// Nil errors delegate.
type w17PlaceholderWedgeFs struct {
	afero.Fs
	path       string
	takeover   string
	closeErr   error
	restoreErr error
	removeErr  error
	armed      atomic.Value // string: the vacate-armed terminal name
}

func (f *w17PlaceholderWedgeFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if f.closeErr != nil && name == f.path && flag&os.O_EXCL != 0 {
		return &w17CloseFailFile{File: file, err: f.closeErr}, nil
	}
	return file, nil
}

func (f *w17PlaceholderWedgeFs) Rename(oldpath, newpath string) error {
	if f.restoreErr != nil && oldpath == f.takeover && newpath == f.path {
		return f.restoreErr
	}
	err := f.Fs.Rename(oldpath, newpath)
	if err == nil && f.removeErr != nil && strings.Contains(newpath, ".vac.") {
		f.armed.Store(newpath)
	}
	return err
}

func (f *w17PlaceholderWedgeFs) Remove(name string) error {
	if f.removeErr != nil && name == f.armed.Load() {
		return f.removeErr
	}
	return f.Fs.Remove(name)
}

// Close fails, restore succeeds: the displaced marker content lands back on
// the marker path over the placeholder, the takeover file is consumed, and
// NO zero-byte malformed marker remains. The original close error surfaces.
func TestReplacementBusyW17T_PlaceholderCloseFailureRestoresDisplacedMarker(t *testing.T) {
	base, path, takeover, content := newW28TakeoverFixture(t, false)
	closeErr := errors.New("w17t placeholder close wedged")
	fs := &w17PlaceholderWedgeFs{Fs: base, path: path, takeover: takeover, closeErr: closeErr}

	err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.ErrorIs(t, err, closeErr)
	require.ErrorContains(t, err, "close replacement busy restore placeholder")

	got, readErr := afero.ReadFile(base, path)
	require.NoError(t, readErr)
	require.Equal(t, content, got,
		"the displaced marker bytes are restored over the placeholder — no tokenless zero-byte marker remains")
	require.NotZero(t, len(got))
	_, statErr := base.Stat(takeover)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the restore consumed the takeover file")
}

// Close fails AND the restore fails: the placeholder is REMOVED (an absent
// marker self-heals; a malformed one never does), the takeover bytes stay in
// their uniquely-named sibling for inspection, the close error still
// surfaces, and a subsequent acquire works — no permanent block.
func TestReplacementBusyW17T_CloseAndRestoreFailRemovesPlaceholder(t *testing.T) {
	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	base, path, takeover, content := newW28TakeoverFixture(t, false)
	closeErr := errors.New("w17t placeholder close wedged")
	restoreErr := errors.New("w17t takeover restore wedged")
	fs := &w17PlaceholderWedgeFs{Fs: base, path: path, takeover: takeover, closeErr: closeErr, restoreErr: restoreErr}

	err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.ErrorIs(t, err, closeErr)

	_, statErr := base.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"the unclosed placeholder is removed — no malformed permanent marker")
	got, readErr := afero.ReadFile(base, takeover)
	require.NoError(t, readErr)
	require.Equal(t, content, got,
		"the displaced bytes stay recoverable at the uniquely-named takeover sibling")
	require.Contains(t, logs.String(), "removing the placeholder")

	// The destination's marker name is free again: a subsequent acquire works.
	release, acquireErr := AcquireReplacementBusy(base, "/out/w28-helper/poster.jpg")
	require.NoError(t, acquireErr, "no permanent busy-block after the recovery")
	release()
	_, statErr = base.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the released fresh marker cleans up")
}

// Close, restore AND placeholder removal all fail: both failures reach the
// warn seam, the close error still surfaces, and the residual state is the
// documented one (placeholder + takeover bytes both retained for manual
// cleanup) — never a silent strand.
func TestReplacementBusyW17T_AllRecoveriesFailWarnAndSurface(t *testing.T) {
	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	base, path, takeover, content := newW28TakeoverFixture(t, false)
	closeErr := errors.New("w17t placeholder close wedged")
	restoreErr := errors.New("w17t takeover restore wedged")
	removeErr := errors.New("w17t placeholder remove wedged")
	fs := &w17PlaceholderWedgeFs{Fs: base, path: path, takeover: takeover,
		closeErr: closeErr, restoreErr: restoreErr, removeErr: removeErr}

	err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.ErrorIs(t, err, closeErr)

	require.Contains(t, logs.String(), "removing the placeholder")
	require.Contains(t, logs.String(), "could not be removed")
	info, statErr := base.Stat(path)
	require.NoError(t, statErr, "with every recovery wedged the placeholder remains for manual cleanup")
	require.Zero(t, info.Size(), "it is the known zero-byte placeholder, documented residual")
	got, readErr := afero.ReadFile(base, takeover)
	require.NoError(t, readErr)
	require.Equal(t, content, got, "the displaced bytes are never lost")
}
