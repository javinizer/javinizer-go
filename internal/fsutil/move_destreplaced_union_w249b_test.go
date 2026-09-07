//go:build !windows

package fsutil

// PR #249 codex P2 follow-up (F3) — displacement evidence accumulated by the
// publish closure across PublishStagedBound attempts must UNION with the
// post-publish error paths: a publish leg that LANDED (displacing a resident
// at the destination) followed by a post-publish identity break or a
// republish-budget refusal must NOT return destReplaced=false — the resident
// bytes are destroyed either way, and the audit crumb keys on that
// destruction. The pre-fix staging core answered false on EVERY error leg,
// erasing a proven displacement.
//
// Both tests drive the real OsFs staged publish (the only leg with the
// publish-with-reverify loop) and wedge the post-publish destination
// identity lookup (publishStagedBoundDestLstat, the wave-24/26 seam) to
// fail once — after the publish landed over an OCCUPIED destination. The
// copy FileInfo must then carry displacedOccupant=true THROUGH the
// ErrPublishStagedIdentityBreak error, for CopyFileFsDestReplaced directly
// AND for MoveFileFsDestReplaced's EXDEV cross-device fallback (its copy-leg
// error branch used to hard-drop the unioned answer with `return false`).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wedgeFirstDestReverify arms the publishStagedBoundDestLstat seam so the
// FIRST lookup naming dst fails with a transient error (the post-publish
// identity break leg) and every subsequent lookup delegates. The arming
// count proves the wedge really fired at the intended call.
func wedgeFirstDestReverify(t *testing.T, dst string) *atomic.Int64 {
	t.Helper()
	prev := publishStagedBoundDestLstat
	var armed atomic.Int64
	var lookups atomic.Int64
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if filepath.Clean(name) == filepath.Clean(dst) && armed.CompareAndSwap(1, 0) {
			lookups.Add(1)
			return nil, &os.PathError{Op: "lstat", Path: name, Err: syscall.EIO}
		}
		return prev(name)
	}
	armed.Store(1)
	t.Cleanup(func() { publishStagedBoundDestLstat = prev })
	return &lookups
}

func TestCopyFileFsDestReplaced_PostPublishIdentityBreak_KeepsDisplacementEvidence(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "src.txt")
	dst := filepath.Join(dir, "out", "dst.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("winner-bytes"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("resident-bytes"), 0o644))
	lookups := wedgeFirstDestReverify(t, dst)

	replaced, err := CopyFileFsDestReplaced(afero.NewOsFs(), src, dst)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPublishStagedIdentityBreak),
		"the post-publish identity lookup was wedged — the publish itself already landed")
	require.Equal(t, int64(1), lookups.Load(), "the wedge fired exactly at the first post-publish reverify")
	assert.True(t, replaced,
		"a publish leg that displaced resident bytes before the post-publish failure must NOT return false")

	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr, "the publish landed — the destination carries this operation's bytes")
	assert.Equal(t, []byte("winner-bytes"), content, "the resident bytes were really displaced")
	retained, readErr := os.ReadFile(src)
	require.NoError(t, readErr, "a copy retains its source")
	assert.Equal(t, []byte("winner-bytes"), retained)
}

// exdevStagedCreateWedgeFs drives the move fallback's PRE-publish copy-leg
// failure by name: the exact (src, dst) rename refuses EXDEV (forcing the
// cross-device fallback) and the staged O_EXCL creation refuses. The union
// line in crossDeviceMoveFsDestReplaced then executes with legitimately
// ZERO displacement evidence — nothing ever published, so the resident
// occupant stays byte-intact and the answer stays false.
type exdevStagedCreateWedgeFs struct {
	afero.Fs
	src, dst string
}

func (w *exdevStagedCreateWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(w.src) && filepath.Clean(newname) == filepath.Clean(w.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return w.Fs.Rename(oldname, newname)
}

func (w *exdevStagedCreateWedgeFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(filepath.Base(name), ".mvstg") && flag&os.O_CREATE != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: syscall.EACCES}
	}
	return w.Fs.OpenFile(name, flag, perm)
}

func TestMoveFileFsDestReplaced_EXDEVCopyLegPrepublishFailure_AnswersNoDisplacement(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/out/dst.txt", []byte("resident-bytes"), 0o644))
	wedge := &exdevStagedCreateWedgeFs{Fs: base, src: "/in/src.txt", dst: "/out/dst.txt"}

	replaced, err := MoveFileFsDestReplaced(wedge, "/in/src.txt", "/out/dst.txt")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPublishCompleted),
		"nothing ever published — no publish-completed typing")
	assert.False(t, replaced,
		"the union only ever carries PROVEN displacement: a pre-publish copy-leg failure answers false, unchanged")

	content, readErr := afero.ReadFile(base, "/out/dst.txt")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("resident-bytes"), content, "the resident occupant is untouched (#224 keep-both)")
	retained, readErr := afero.ReadFile(base, "/in/src.txt")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("winner-bytes"), retained)
}
