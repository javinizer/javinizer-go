package downloader

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6qKDM5): a published soft link recorded by the
// name-based observation rolls back through the link-object unlink — never
// stranded with an "foreign bytes preserved" error because the regular-file
// comparison has no link model.
func TestReplacementBatchRollbackSoftLinkLeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a symlink-capable test host without elevation")
	}
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	b, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = b.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(src, dst))
	b.ObservePublishResult(dst)
	require.NoError(t, b.ConfirmPublish(context.Background(), dst))
	require.NoError(t, b.Rollback(context.Background()), "the soft-link leg rolls back like any installed output")
	exists, err := afero.Exists(fs, dst)
	require.NoError(t, err)
	assert.False(t, exists, "the link object is removed")
	exists, err = afero.Exists(fs, src)
	require.NoError(t, err)
	assert.True(t, exists, "the source is untouched")
}

// The refusal twin: a foreign link replanted over the name between the
// observation and the rollback is preserved, never unlinked while carrying a
// payload the record did not capture.
func TestReplacementBatchRollbackSoftLinkLegRetainsForeignLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a symlink-capable test host without elevation")
	}
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	b, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	_, err = b.BeforePublish(context.Background(), dst, false)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(src, dst))
	b.ObservePublishResult(dst)
	require.NoError(t, b.ConfirmPublish(context.Background(), dst))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("f"), 0o644))
	require.NoError(t, os.Remove(dst))
	require.NoError(t, os.Symlink(foreign, dst), "a foreign link (different payload) replants the name")

	rerr := b.Rollback(context.Background())
	require.Error(t, rerr, "the foreign link is unprovable — the refusal is reported, not silent")
	link, lerr := os.Readlink(dst)
	require.NoError(t, lerr)
	assert.Equal(t, foreign, link, "the foreign link is retained byte-intact")
}
