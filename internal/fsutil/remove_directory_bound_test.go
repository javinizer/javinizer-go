package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type boundDirectoryFaultRoot struct {
	boundDirectoryRootInterface
	openErr   error
	statErr   error
	readErr   error
	removeErr error
	foreign   string
}

func (r boundDirectoryFaultRoot) openDirectory() (afero.File, error) {
	if r.openErr != nil {
		return nil, r.openErr
	}
	if r.foreign != "" {
		return os.Open(r.foreign)
	}
	file, err := r.boundDirectoryRootInterface.openDirectory()
	if err != nil {
		return nil, err
	}
	return boundDirectoryFaultFile{File: file, statErr: r.statErr, readErr: r.readErr}, nil
}

func (r boundDirectoryFaultRoot) RemoveAll(path string) error {
	if r.removeErr != nil {
		return r.removeErr
	}
	return r.boundDirectoryRootInterface.RemoveAll(path)
}

type boundDirectoryFaultFile struct {
	afero.File
	statErr, readErr error
}

func (f boundDirectoryFaultFile) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }
func (f boundDirectoryFaultFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.File.Stat()
}
func (f boundDirectoryFaultFile) Readdirnames(count int) ([]string, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.File.Readdirnames(count)
}

func boundDirectoryFixture(t *testing.T) (afero.Fs, string, uint64, uint64) {
	t.Helper()
	fs := afero.NewOsFs()
	root := filepath.Join(t.TempDir(), "owned")
	require.NoError(t, fs.MkdirAll(filepath.Join(root, "nested"), 0o700))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "nested", "owned.bin"), []byte("owned"), 0o600))
	file, err := fs.Open(root)
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	dev, ino, ok := streamedHandleIdentity(file, info)
	require.True(t, ok)
	require.NoError(t, file.Close())
	return fs, root, dev, ino
}

type finalBoundDirectoryFS struct {
	afero.Fs
	root    string
	mode    string
	lookups int
}

func (f *finalBoundDirectoryFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if path == f.root {
		f.lookups++
		if f.lookups == 2 {
			if f.mode == "lookup refusal" {
				return nil, false, errors.New("final lookup refused")
			}
			if f.mode == "successor" {
				if err := f.Fs.Rename(path, path+".retained"); err != nil {
					return nil, false, err
				}
				if err := f.Fs.MkdirAll(path, 0o700); err != nil {
					return nil, false, err
				}
				if err := afero.WriteFile(f.Fs, filepath.Join(path, "foreign.bin"), []byte("foreign"), 0o600); err != nil {
					return nil, false, err
				}
			}
		}
	}
	info, err := f.Fs.Stat(path)
	return info, false, err
}

func (f *finalBoundDirectoryFS) Remove(path string) error {
	if path == f.root && f.mode == "remove refusal" {
		return errors.New("final remove refused")
	}
	return f.Fs.Remove(path)
}

func TestAferoRemoveAllBoundDirectory(t *testing.T) {
	for _, mode := range []string{"success", "wrong identity", "lookup refusal", "successor", "remove refusal"} {
		t.Run(mode, func(t *testing.T) {
			base, root, dev, ino := boundDirectoryFixture(t)
			fs := &finalBoundDirectoryFS{Fs: base, root: root, mode: mode}
			if mode == "wrong identity" {
				dev++
			}
			err := AferoRemoveAll(fs, root, [2]uint64{dev, ino})
			if mode == "success" {
				require.NoError(t, err)
				_, statErr := base.Stat(root)
				require.ErrorIs(t, statErr, os.ErrNotExist)
				return
			}
			require.Error(t, err)
			switch mode {
			case "wrong identity":
				require.ErrorIs(t, err, ErrTakeAsideForeign)
				body, readErr := afero.ReadFile(base, filepath.Join(root, "nested", "owned.bin"))
				require.NoError(t, readErr)
				assert.Equal(t, "owned", string(body))
			case "successor":
				require.ErrorIs(t, err, ErrTakeAsideForeign)
				body, readErr := afero.ReadFile(base, filepath.Join(root, "foreign.bin"))
				require.NoError(t, readErr)
				assert.Equal(t, "foreign", string(body))
			case "lookup refusal":
				require.ErrorContains(t, err, "final lookup refused")
			case "remove refusal":
				require.ErrorContains(t, err, "final remove refused")
			}
			_, statErr := base.Stat(root)
			require.NoError(t, statErr)
		})
	}
}

func TestBoundDirectoryContentsRemoveNestedOwnedTree(t *testing.T) {
	fs, root, dev, ino := boundDirectoryFixture(t)
	require.NoError(t, removeDirectoryContentsBound(fs, root, dev, ino))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestBoundDirectoryRetainsReplacementAfterLastProof(t *testing.T) {
	fs, root, dev, ino := boundDirectoryFixture(t)
	original := removeBoundDirectoryChild
	t.Cleanup(func() { removeBoundDirectoryChild = original })
	swapped := false
	removeBoundDirectoryChild = func(pinned boundDirectoryRootInterface, child string) error {
		if !swapped {
			require.NoError(t, fs.Rename(root, root+".retained"))
			require.NoError(t, fs.MkdirAll(filepath.Join(root, "nested"), 0o700))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "nested", "foreign.bin"), []byte("foreign nested bytes"), 0o600))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "foreign.bin"), []byte("foreign root bytes"), 0o600))
			swapped = true
		}
		return original(pinned, child)
	}
	require.NoError(t, removeDirectoryContentsBound(fs, root, dev, ino))
	require.True(t, swapped)
	for name, want := range map[string]string{"foreign.bin": "foreign root bytes", filepath.Join("nested", "foreign.bin"): "foreign nested bytes"} {
		body, err := afero.ReadFile(fs, filepath.Join(root, name))
		require.NoError(t, err)
		assert.Equal(t, want, string(body))
	}
	entries, err := os.ReadDir(root + ".retained")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestBoundDirectoryRejectsWrongAdmissionAndMissingRoot(t *testing.T) {
	fs, root, dev, ino := boundDirectoryFixture(t)
	require.ErrorIs(t, removeDirectoryContentsBound(fs, root, dev+1, ino), ErrTakeAsideForeign)
	require.Error(t, removeDirectoryContentsBound(fs, filepath.Join(root, "missing"), dev, ino))
	body, err := afero.ReadFile(fs, filepath.Join(root, "nested", "owned.bin"))
	require.NoError(t, err)
	assert.Equal(t, "owned", string(body))
}

func TestBoundDirectoryCapabilityFaultsRetainOwnedTree(t *testing.T) {
	original := openBoundDirectoryRoot
	defer func() { openBoundDirectoryRoot = original }()
	for _, point := range []string{"directory open", "directory stat", "foreign descriptor", "named open", "named stat", "named identity", "namespace lookup", "namespace identity", "directory read", "child remove"} {
		t.Run(point, func(t *testing.T) {
			base, root, dev, ino := boundDirectoryFixture(t)
			foreign := filepath.Join(t.TempDir(), "foreign")
			require.NoError(t, base.MkdirAll(foreign, 0o700))
			sentinel := errors.New("capability refused " + point)
			fs := &verifyHookFS{Fs: base}
			openBoundDirectoryRoot = func(path string) (boundDirectoryRootInterface, error) {
				raw, err := original(path)
				if err != nil {
					return nil, err
				}
				fault := boundDirectoryFaultRoot{boundDirectoryRootInterface: raw}
				switch point {
				case "directory open":
					fault.openErr = sentinel
				case "directory stat":
					fault.statErr = sentinel
				case "foreign descriptor":
					fault.foreign = foreign
				case "directory read":
					fault.readErr = sentinel
				case "child remove":
					fault.removeErr = sentinel
				}
				return fault, nil
			}
			switch point {
			case "named open":
				fs.open = func(string) (afero.File, error) { return nil, sentinel }
			case "named stat":
				fs.open = func(path string) (afero.File, error) {
					file, err := base.Open(path)
					return boundDirectoryFaultFile{File: file, statErr: sentinel}, err
				}
			case "named identity":
				fs.open = func(string) (afero.File, error) { return base.Open(foreign) }
			case "namespace lookup":
				fs.lstat = func(string) (os.FileInfo, bool, error) { return nil, false, sentinel }
			case "namespace identity":
				fs.lstat = func(path string) (os.FileInfo, bool, error) {
					info, err := base.Stat(path)
					return syslessInfo{info}, false, err
				}
			}
			err := removeDirectoryContentsBound(fs, root, dev, ino)
			require.Error(t, err)
			if point == "foreign descriptor" || point == "named identity" || point == "namespace identity" {
				require.ErrorIs(t, err, ErrTakeAsideForeign)
			} else {
				require.ErrorIs(t, err, sentinel)
			}
			body, err := afero.ReadFile(base, filepath.Join(root, "nested", "owned.bin"))
			require.NoError(t, err)
			assert.Equal(t, "owned", string(body))
		})
	}
}
