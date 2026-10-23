package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestIssue277CheckedReplacementReprovesTakenObject(t *testing.T) {
	fs := afero.NewOsFs()
	target := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(target, []byte("reviewed"), 0644))
	info, err := os.Stat(target)
	require.NoError(t, err)
	dev, ino, strong := fsutil.BoundObjectIdentity(fs, target, info)
	require.True(t, strong)
	stale := errors.New("stale reviewed target")
	recorder := &replacementBatchRecorder{}
	batch, err := NewReplacementBatch(fs, "issue277", recorder)
	require.NoError(t, err)
	calls := 0
	_, err = batch.BeforePublishChecked(t.Context(), target, true, func(path string) error {
		calls++
		current, err := os.Lstat(path)
		if err != nil {
			return stale
		}
		device, inode, ok := fsutil.BoundObjectIdentity(fs, path, current)
		if !ok || device != dev || inode != ino {
			return stale
		}
		if calls == 1 {
			require.NoError(t, os.Rename(target, target+".reviewed"))
			require.NoError(t, os.WriteFile(target, []byte("late writer"), 0644))
		}
		return nil
	})
	require.ErrorIs(t, err, stale)
	require.Equal(t, 2, calls)
	require.Equal(t, 1, recorder.recorded)
	require.NoError(t, batch.Rollback(t.Context()))
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "late writer", string(body))
	body, err = os.ReadFile(target + ".reviewed")
	require.NoError(t, err)
	require.Equal(t, "reviewed", string(body))
}

func TestIssue277CheckedReplacementMissingParentStillVerifies(t *testing.T) {
	batch, err := NewReplacementBatch(afero.NewOsFs(), "issue277", &replacementBatchRecorder{})
	require.NoError(t, err)
	stale := errors.New("reviewed destination vanished")
	calls := 0
	_, err = batch.BeforePublishChecked(t.Context(), filepath.Join(t.TempDir(), "missing", "video.mp4"), true, func(string) error { calls++; return stale })
	require.ErrorIs(t, err, stale)
	require.Equal(t, 1, calls)
}
