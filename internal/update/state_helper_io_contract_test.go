package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// These exercise private test-support helpers, not stateStore's public service
// path. Faults stay on an owned filesystem instance and never touch globals.
type stateHelperIOFaultFS struct {
	afero.Fs
	path        string
	readErr     error
	writeErr    error
	renameCalls int
}

func (f *stateHelperIOFaultFS) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == f.path && f.readErr != nil {
		return nil, f.readErr
	}
	return f.Fs.Open(name)
}

func (f *stateHelperIOFaultFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == f.path+".tmp" && f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *stateHelperIOFaultFS) Rename(oldname, newname string) error {
	f.renameCalls++
	return f.Fs.Rename(oldname, newname)
}

func TestLoadStateHelperPropagatesReadFailureAndRecovers(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := filepath.Join("cache", "state.json")
	want := &updateState{Version: "v1.2.3", Source: UpdateSourceCached, ETag: "original"}
	require.NoError(t, saveStateToFile(fs, path, want))
	before, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	failure := &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	faults := &stateHelperIOFaultFS{Fs: fs, path: path, readErr: failure}

	got, err := loadStateFromFile(faults, path)
	require.Nil(t, got)
	require.Same(t, failure, err)
	require.ErrorIs(t, err, os.ErrPermission)
	after, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// A missing file remains distinguishable from a failed read.
	missing, err := loadStateFromFile(faults, filepath.Join("cache", "missing.json"))
	require.NoError(t, err)
	require.Nil(t, missing)
	faults.readErr = nil
	got, err = loadStateFromFile(faults, path)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSaveStateHelperWriteFailurePreservesPriorFileAndRecovers(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := filepath.Join("cache", "state.json")
	original := &updateState{Version: "v1.2.3", Source: UpdateSourceCached, ETag: "original"}
	require.NoError(t, saveStateToFile(fs, path, original))
	before, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	failure := &os.PathError{Op: "open", Path: path + ".tmp", Err: os.ErrPermission}
	faults := &stateHelperIOFaultFS{Fs: fs, path: path, writeErr: failure}
	replacement := &updateState{Version: "v2.3.4", Source: UpdateSourceFresh, ETag: "replacement"}

	err = saveStateToFile(faults, path, replacement)
	require.Same(t, failure, err)
	require.ErrorIs(t, err, os.ErrPermission)
	require.Zero(t, faults.renameCalls, "a failed temporary write must not publish")
	after, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	got, err := loadStateFromFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, original, got)
	_, err = fs.Stat(path + ".tmp")
	require.True(t, os.IsNotExist(err), "the rejected open must not create a temporary file")

	faults.writeErr = nil
	require.NoError(t, saveStateToFile(faults, path, replacement))
	require.Equal(t, 1, faults.renameCalls)
	got, err = loadStateFromFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, replacement, got)
	_, err = fs.Stat(path + ".tmp")
	require.True(t, os.IsNotExist(err), "successful publication consumes the temporary file")
}
