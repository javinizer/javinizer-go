package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type cleanupRefusalFS struct {
	afero.Fs
	refused  string
	attempts []string
}

func (fs *cleanupRefusalFS) Remove(path string) error {
	fs.attempts = append(fs.attempts, path)
	if path == fs.refused {
		return &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission}
	}
	return fs.Fs.Remove(path)
}

func TestCleanupPosterPathsContinuesAfterRemovalRefusal(t *testing.T) {
	fs := &cleanupRefusalFS{Fs: afero.NewMemMapFs()}
	dir := filepath.Join("temp", "posters")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	refused := filepath.Join(dir, "protected.jpg")
	removable := filepath.Join(dir, "obsolete.jpg")
	unrelated := filepath.Join(dir, "keep.jpg")
	missing := filepath.Join(dir, "absent.jpg")
	fs.refused = refused
	for _, path := range []string{refused, removable, unrelated} {
		require.NoError(t, afero.WriteFile(fs, path, []byte(path), 0o644))
	}

	CleanupPosterPaths(fs, []string{refused, missing, removable})

	require.Equal(t, []string{refused, removable}, fs.attempts)
	for _, path := range []string{refused, unrelated} {
		bytes, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		require.Equal(t, []byte(path), bytes)
	}
	_, err := fs.Stat(removable)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat(missing)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCleanJobTempDirRemovalRefusalPreservesOtherJobs(t *testing.T) {
	fs := &cleanupRefusalFS{Fs: afero.NewMemMapFs()}
	jobID, otherJobID := models.NewJobID(), models.NewJobID()
	jobDir := filepath.Join("temp", "posters", jobID.String())
	refused := filepath.Join(jobDir, "poster.jpg")
	unrelated := filepath.Join("temp", "posters", otherJobID.String(), "poster.jpg")
	fs.refused = refused
	for _, path := range []string{refused, unrelated} {
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(fs, path, []byte(path), 0o644))
	}
	cleaner := NewTempDirCleaner(fs, "temp", nil)
	cleaner.CleanJobTempDir(jobID.String())

	require.Equal(t, []string{refused}, fs.attempts)
	for _, path := range []string{refused, unrelated} {
		bytes, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		require.Equal(t, []byte(path), bytes)
	}
	// A later cleanup can recover from the transient refusal without touching
	// the neighboring job. No permissions or platform-specific chmod needed.
	fs.refused = ""
	cleaner.CleanJobTempDir(jobID.String())
	_, err := fs.Stat(jobDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	bytes, err := afero.ReadFile(fs, unrelated)
	require.NoError(t, err)
	require.Equal(t, []byte(unrelated), bytes)
}
