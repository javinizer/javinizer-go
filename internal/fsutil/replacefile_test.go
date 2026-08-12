package fsutil

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rejectExistingRenameFS mimics a Windows-style rename that refuses existing
// destinations (ReplaceFile on virtual FS must surface the failure and keep
// the pre-existing bytes).
type rejectExistingRenameFS struct {
	afero.Fs
}

func (f rejectExistingRenameFS) Rename(oldname, newname string) error {
	if _, err := f.Fs.Stat(newname); err == nil {
		return errors.New("replace existing destination rejected")
	}
	return f.Fs.Rename(oldname, newname)
}

type recordingRenameFS struct {
	afero.Fs
	calls atomic.Int32
}

func (f *recordingRenameFS) Rename(oldname, newname string) error {
	f.calls.Add(1)
	return f.Fs.Rename(oldname, newname)
}

func TestReplaceFile_ReplacesExistingOnVirtualFS(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/source.tmp", []byte("new"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/output/destination.jpg", []byte("old"), 0644))

	require.NoError(t, ReplaceFile(fs, "/output/source.tmp", "/output/destination.jpg"))
	got, err := afero.ReadFile(fs, "/output/destination.jpg")
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
	_, err = fs.Stat("/output/source.tmp")
	assert.True(t, os.IsNotExist(err))
}

func TestReplaceFile_DispatchesToWrappedBackend(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &recordingRenameFS{Fs: base}
	require.NoError(t, afero.WriteFile(fs, "/output/source.tmp", []byte("new"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/output/destination.jpg", []byte("old"), 0644))

	require.NoError(t, ReplaceFile(fs, "/output/source.tmp", "/output/destination.jpg"))
	assert.Equal(t, int32(1), fs.calls.Load())
	got, err := afero.ReadFile(base, "/output/destination.jpg")
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
}

func TestReplaceFile_FailurePreservesDestination(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := rejectExistingRenameFS{Fs: base}
	require.NoError(t, afero.WriteFile(base, "/output/source.tmp", []byte("new"), 0644))
	old := []byte("old")
	require.NoError(t, afero.WriteFile(base, "/output/destination.jpg", old, 0644))

	err := ReplaceFile(fs, "/output/source.tmp", "/output/destination.jpg")
	require.Error(t, err)
	got, readErr := afero.ReadFile(base, "/output/destination.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, old, got)
}
