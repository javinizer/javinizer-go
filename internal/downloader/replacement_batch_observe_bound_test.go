package downloader

// codex P1 (PR #276, finding ntCe6) — the batch's bound observation: an
// identity produced by the publish itself is what a leg may adopt; an
// affirmative successor keeps the leg UNINSTALLED (rollback skips it, the
// foreign bytes survive), and ConfirmPublish never re-anchors an already
// bound leg to whatever occupies the name after the observation window.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestReplacementBatchBoundObserveAdoptsPublishIdentity(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	replaced, err := batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)
	require.False(t, replaced, "absent destination arms as created")

	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.NoError(t, batch.ObservePublishResultBound(dst, identity))
	require.NoError(t, batch.ConfirmPublish(context.Background(), dst))

	// Rollback of a committed-then-compensated leg deletes OUR install only.
	require.NoError(t, batch.Rollback(context.Background()))
	exists, _ := afero.Exists(fs, dst)
	assert.False(t, exists, "rollback removes the batch's own verified install")
}

func TestReplacementBatchBoundObserveSuccessorNeverInstalledNorDeleted(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)

	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	// External replacement inside the lock-release→observe window.
	require.NoError(t, fs.Remove(dst))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign successor"), 0o644))

	oerr := batch.ObservePublishResultBound(dst, identity)
	require.Error(t, oerr)
	assert.ErrorIs(t, oerr, fsutil.ErrPublishSuccessorUnproven)
	assert.ErrorIs(t, oerr, fsutil.ErrPublishCompleted)

	// The leg never installed: rollback skips it entirely (nil rollback), and
	// the foreign occupant survives byte-intact.
	require.NoError(t, batch.Rollback(context.Background()))
	got, rerr := afero.ReadFile(fs, dst)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign successor", string(got))
}

func TestReplacementBatchConfirmKeepsBoundIdentityAcrossObservationGap(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)

	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, identity))
	// Successor lands AFTER the bound observation but BEFORE confirmation —
	// the observe→confirm gap a name-derived refresh would re-anchor on.
	require.NoError(t, fs.Remove(dst))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("gap window successor"), 0o644))
	require.NoError(t, batch.ConfirmPublish(context.Background(), dst))

	rerr := batch.Rollback(context.Background())
	require.Error(t, rerr, "rollback must refuse to unlink the successor the bound identity never described")
	assert.ErrorIs(t, rerr, fsutil.ErrTakeAsideForeign, "the bound unlink preserves the foreign occupant: %v", rerr)
	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "gap window successor", string(got), "gap-window successor retained byte-intact")
}

func TestReplacementBatchBoundObserveVacantRefusedStaysUninstalled(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)

	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, fs.Remove(dst))

	// codex P1, PRRT_kwDORn9KaM6qJY2i: the vacant name is refused (doubt class),
	// never adopted — and the leg stays uninstalled so no later rollback can be
	// armed against whatever occupies the endpoint.
	obsErr := batch.ObservePublishResultBound(dst, identity)
	require.Error(t, obsErr, "a vacant name after a proven publish is refused")
	assert.True(t, errors.Is(obsErr, fsutil.ErrPublishCompleted), "the doubt class rides the refusal")
	assert.False(t, errors.Is(obsErr, fsutil.ErrPublishSuccessorUnproven), "absence is doubt, not divergence")
	require.Error(t, batch.ConfirmPublish(context.Background(), dst), "the leg stays uninstalled")
}

// codex P1 (PRRT_kwDORn9KaM6qJY2g): the rollback MOVE-BACK is identity-bound. A
// destination swapped by another writer after the publish marker was released
// must be retained byte-intact — never relocated onto the original source.
func TestReplacementBatchRollbackMoveBackRefusesForeignSuccessor(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	origin := filepath.Join(dir, "origin.mp4")
	require.NoError(t, afero.WriteFile(fs, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)
	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, identity))
	require.NoError(t, batch.SetRollbackOrigin(dst, origin))

	// The intruder: same name, different inode and bytes, after the marker.
	require.NoError(t, fs.Remove(dst))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign successor"), 0o644))

	rerr := batch.Rollback(context.Background())
	require.Error(t, rerr, "the skipped compensation is reported, not silently ignored")
	assert.True(t, errors.Is(rerr, fsutil.ErrTakeAsideForeign) || errors.Is(rerr, fsutil.ErrPublishSuccessorUnproven) || errors.Is(rerr, fsutil.ErrPublishCompleted), "the bound move-back refusal rides the skipped compensation: %v", rerr)
	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "foreign successor", string(got), "the successor is retained byte-intact at the destination")
	_, statErr := fs.Stat(origin)
	assert.True(t, os.IsNotExist(statErr), "no foreign bytes were relocated onto the source path")
}

type rollbackMoveBackSwapFS struct {
	afero.Fs
	source string
	done   bool
}

func (fs *rollbackMoveBackSwapFS) Rename(oldname, newname string) error {
	if !fs.done && filepath.Clean(oldname) == filepath.Clean(fs.source) {
		fs.done = true
		if err := fs.Fs.Remove(oldname); err != nil {
			return err
		}
		if err := afero.WriteFile(fs.Fs, oldname, []byte("foreign in move window"), 0o644); err != nil {
			return err
		}
	}
	return fs.Fs.Rename(oldname, newname)
}

// codex P1 (PRRT_kwDORn9KaM6qLkJ9): the rollback move-back itself re-proves
// the taken object, so a swap after the old observation point cannot be moved
// onto the rollback origin.
func TestReplacementBatchRollbackMoveBackBindsProofToMove(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	origin := filepath.Join(dir, "origin.mp4")
	require.NoError(t, afero.WriteFile(base, src, []byte("ours"), 0o644))
	batch, err := NewReplacementBatch(&rollbackMoveBackSwapFS{Fs: base, source: dst}, "op", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)
	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(base, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, identity))
	require.NoError(t, batch.SetRollbackOriginBound(dst, origin, identity))

	rerr := batch.Rollback(context.Background())
	require.Error(t, rerr)
	assert.ErrorIs(t, rerr, fsutil.ErrTakeAsideForeign)
	got, readErr := afero.ReadFile(base, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "foreign in move window", string(got))
	_, statErr := base.Stat(origin)
	assert.True(t, os.IsNotExist(statErr), "foreign bytes were not moved onto origin")
}
