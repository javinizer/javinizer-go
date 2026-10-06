package downloader

// codex P1 (PR #276, finding ntCe6) — the batch's bound observation: an
// identity produced by the publish itself is what a leg may adopt; an
// affirmative successor keeps the leg UNINSTALLED (rollback skips it, the
// foreign bytes survive), and ConfirmPublish never re-anchors an already
// bound leg to whatever occupies the name after the observation window.

import (
	"context"
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

func TestReplacementBatchBoundObserveVacantStaysUninstalled(t *testing.T) {
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

	require.NoError(t, batch.ObservePublishResultBound(dst, identity), "a vacant name is doubt without divergence")
	require.Error(t, batch.ConfirmPublish(context.Background(), dst), "confirmation surfaces the vanished install, mirroring the legacy name-based flow")
}
