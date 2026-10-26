package downloader

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func requireAbsentParentDestinationUnlocked(t *testing.T, destination string) {
	t.Helper()
	unlocked := make(chan struct{})
	go func() {
		release := fsutil.SharedDestLocks().Acquire(destination)
		release()
		close(unlocked)
	}()
	select {
	case <-unlocked:
	case <-time.After(5 * time.Second):
		t.Fatal("absent-parent publication retained its destination lock")
	}
}

func TestReplacementBatchAbsentParentPublishRollback(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	parent := filepath.Join(root, "not-created-yet")
	destination := filepath.Join(parent, "poster.jpg")
	foreign := filepath.Join(parent, "foreign.tmp")
	recorder := &replacementBatchRecorder{}
	batch, err := NewReplacementBatch(fs, "absent-parent", recorder)
	require.NoError(t, err)
	require.NoError(t, batch.Preflight([]string{destination}))
	replaced, err := batch.BeforePublish(t.Context(), destination, false)
	require.NoError(t, err)
	require.False(t, replaced)
	require.False(t, batch.IsReplacement(destination))
	// Arming an absent destination neither creates its parent nor invents a
	// backup/replacement journal. The real publisher creates the directory.
	_, err = fs.Stat(parent)
	require.ErrorIs(t, err, os.ErrNotExist)
	t.Cleanup(func() { _ = batch.ReleaseUninstalled(destination) })
	require.NoError(t, fs.MkdirAll(parent, 0o755))
	require.NoError(t, afero.WriteFile(fs, foreign, []byte("unowned neighboring staging"), 0o600))
	require.NoError(t, afero.WriteFile(fs, destination, []byte("newly published bytes"), 0o600))
	require.NoError(t, batch.ConfirmPublish(t.Context(), destination))
	requireAbsentParentDestinationUnlocked(t, destination)
	got, err := afero.ReadFile(fs, destination)
	require.NoError(t, err)
	require.Equal(t, "newly published bytes", string(got))
	require.NoError(t, batch.Rollback(t.Context()))
	_, err = fs.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
	got, err = afero.ReadFile(fs, foreign)
	require.NoError(t, err)
	require.Equal(t, "unowned neighboring staging", string(got))
	entries, err := afero.ReadDir(fs, parent)
	require.NoError(t, err)
	require.Len(t, entries, 1, "rollback must leave only the unowned neighbor, without backup or busy-marker residue")
	require.Equal(t, "foreign.tmp", entries[0].Name())
	require.Zero(t, recorder.recorded)
	require.Zero(t, recorder.confirmed)
	require.Zero(t, recorder.released)
	requireAbsentParentDestinationUnlocked(t, destination)
}

func TestReplacementBatchAbsentParentUnpublishedRelease(t *testing.T) {
	fs := afero.NewMemMapFs()
	parent := filepath.Join(t.TempDir(), "never-created")
	destination := filepath.Join(parent, "poster.jpg")
	recorder := &replacementBatchRecorder{}
	batch, err := NewReplacementBatch(fs, "unpublished-absent-parent", recorder)
	require.NoError(t, err)
	replaced, err := batch.BeforePublish(t.Context(), destination, false)
	require.NoError(t, err)
	require.False(t, replaced)
	t.Cleanup(func() { _ = batch.ReleaseUninstalled(destination) })
	require.NoError(t, batch.ReleaseUninstalled(destination))
	requireAbsentParentDestinationUnlocked(t, destination)
	require.ErrorContains(t, batch.ConfirmPublish(t.Context(), destination), "was not armed")
	require.NoError(t, batch.Rollback(t.Context()))
	_, err = fs.Stat(parent)
	require.ErrorIs(t, err, os.ErrNotExist, "abandoning a never-published leg must not manufacture filesystem objects")
	require.Zero(t, recorder.recorded)
	require.Zero(t, recorder.confirmed)
	require.Zero(t, recorder.released)
}
