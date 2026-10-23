package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestReplacementBatchCheckedDestinationRejectionReleasesCoordination(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	target := filepath.Join(root, "video.mp4")
	require.NoError(t, os.WriteFile(target, []byte("reviewed original"), 0o644))
	recorder := &replacementBatchRecorder{}
	batch, err := NewReplacementBatch(fs, "contract-destination-rejection", recorder)
	require.NoError(t, err)
	stale := errors.New("reviewed destination changed")
	calls := 0
	replaced, err := batch.BeforePublishChecked(t.Context(), target, true, func(path string) error {
		calls++
		require.Equal(t, target, path)
		token, tokenErr := fsutil.ReadReplacementBusyToken(fs, target)
		require.NoError(t, tokenErr)
		require.NotEmpty(t, token)
		body, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, "reviewed original", string(body))
		return stale
	})
	require.ErrorIs(t, err, stale)
	require.False(t, replaced)
	require.Equal(t, 1, calls)
	require.Zero(t, recorder.recorded)
	require.Zero(t, recorder.confirmed)
	require.Empty(t, batch.legs)
	require.False(t, batch.IsReplacement(target))
	require.ErrorContains(t, batch.ConfirmPublish(t.Context(), target), "was not armed")
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "reviewed original", string(body))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "video.mp4", entries[0].Name())

	unlocked := make(chan struct{})
	go func() {
		release := fsutil.SharedDestLocks().Acquire(target)
		release()
		close(unlocked)
	}()
	select {
	case <-unlocked:
	case <-time.After(5 * time.Second):
		t.Fatal("destination verification failure retained its lock")
	}

	replaced, err = batch.BeforePublishChecked(t.Context(), target, true, func(path string) error {
		body, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, "reviewed original", string(body))
		return nil
	})
	require.NoError(t, err)
	require.True(t, replaced)
	require.Equal(t, 1, recorder.recorded)
	require.NoError(t, os.WriteFile(target, []byte("retry publication"), 0o644))
	require.NoError(t, batch.ConfirmPublish(t.Context(), target))
	require.Equal(t, 1, recorder.confirmed)
	body, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "retry publication", string(body))
	require.NoError(t, batch.Rollback(t.Context()))
	require.Equal(t, 1, recorder.released)
	body, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "reviewed original", string(body))
	entries, err = os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "video.mp4", entries[0].Name())
}
