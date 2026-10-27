package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Faults happen at an exact filesystem call, not through native permission
// changes or races. Everything else runs against the real in-memory tree.
type removeAllFailureFS struct {
	afero.Fs
	stat    func(string) error
	open    func(string) error
	remove  func(string) error
	removed []string
}

func (fs *removeAllFailureFS) Stat(name string) (os.FileInfo, error) {
	if fs.stat != nil {
		if err := fs.stat(name); err != nil {
			return nil, err
		}
	}
	return fs.Fs.Stat(name)
}

func (fs *removeAllFailureFS) Open(name string) (afero.File, error) {
	if fs.open != nil {
		if err := fs.open(name); err != nil {
			return nil, err
		}
	}
	return fs.Fs.Open(name)
}

func (fs *removeAllFailureFS) Remove(name string) error {
	fs.removed = append(fs.removed, name)
	if fs.remove != nil {
		if err := fs.remove(name); err != nil {
			return err
		}
	}
	return fs.Fs.Remove(name)
}

func TestAferoRemoveAll_PermissionFailuresContainCleanup(t *testing.T) {
	for _, operation := range []string{"initial stat", "child stat", "directory open", "file remove", "directory remove"} {
		t.Run(operation, func(t *testing.T) {
			base := afero.NewMemMapFs()
			root := filepath.Join(string(filepath.Separator), "cleanup")
			child := filepath.Join(root, "child")
			media := filepath.Join(child, "fixture.txt")
			unrelated := filepath.Join(string(filepath.Separator), "unrelated.txt")
			require.NoError(t, base.MkdirAll(child, 0755))
			require.NoError(t, afero.WriteFile(base, media, []byte("owned fixture"), 0644))
			require.NoError(t, afero.WriteFile(base, unrelated, []byte("untouched"), 0644))
			fs := &removeAllFailureFS{Fs: base}
			faults := 0
			denied := func(target string) func(string) error {
				return func(name string) error {
					if name == target {
						faults++
						return &os.PathError{Op: operation, Path: name, Err: os.ErrPermission}
					}
					return nil
				}
			}
			switch operation {
			case "initial stat":
				fs.stat = denied(root)
			case "child stat":
				fs.stat = denied(media)
			case "directory open":
				fs.open = denied(child)
			case "file remove":
				fs.remove = denied(media)
			case "directory remove":
				fs.remove = denied(child)
			}

			err := AferoRemoveAll(fs, root)
			require.ErrorIs(t, err, os.ErrPermission)
			require.Equal(t, 1, faults, "the configured failure must actually be reached")
			require.NotContains(t, fs.removed, root, "do not remove ancestors after a failure")
			_, err = base.Stat(child)
			require.NoError(t, err, "failed cleanup retains the directory")
			if operation == "directory remove" {
				_, err = base.Stat(media)
				require.ErrorIs(t, err, os.ErrNotExist, "files were removed before the directory failure")
			} else {
				content, err := afero.ReadFile(base, media)
				require.NoError(t, err)
				require.Equal(t, "owned fixture", string(content))
			}
			content, err := afero.ReadFile(base, unrelated)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(content))
			require.NotContains(t, fs.removed, unrelated)
		})
	}
}

func TestAferoRemoveAll_ToleratesDisappearance(t *testing.T) {
	for _, operation := range []string{"child stat", "file remove", "directory remove"} {
		t.Run(operation, func(t *testing.T) {
			base := afero.NewMemMapFs()
			root := filepath.Join(string(filepath.Separator), "cleanup")
			child := filepath.Join(root, "child")
			media := filepath.Join(child, "fixture.txt")
			unrelated := filepath.Join(string(filepath.Separator), "unrelated.txt")
			require.NoError(t, base.MkdirAll(child, 0755))
			require.NoError(t, afero.WriteFile(base, media, []byte("owned fixture"), 0644))
			require.NoError(t, afero.WriteFile(base, unrelated, []byte("untouched"), 0644))
			fs := &removeAllFailureFS{Fs: base}
			faults := 0
			disappear := func(target string) func(string) error {
				return func(name string) error {
					if name != target {
						return nil
					}
					faults++
					// Remove the actual entry before reporting that this operation
					// no longer finds it, as a concurrent remover would do.
					require.NoError(t, base.Remove(name))
					return &os.PathError{Op: operation, Path: name, Err: os.ErrNotExist}
				}
			}
			switch operation {
			case "child stat":
				fs.stat = disappear(media)
			case "file remove":
				fs.remove = disappear(media)
			case "directory remove":
				fs.remove = disappear(child)
			}

			require.NoError(t, AferoRemoveAll(fs, root))
			require.Equal(t, 1, faults)
			_, err := base.Stat(media)
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = base.Stat(root)
			if operation == "file remove" {
				require.NoError(t, err, "walk stops on a removal error, even when disappearance is tolerated")
				require.NotContains(t, fs.removed, root)
			} else {
				require.ErrorIs(t, err, os.ErrNotExist, "walk and directory cleanup can finish after disappearance")
			}
			content, err := afero.ReadFile(base, unrelated)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(content))
			require.NotContains(t, fs.removed, unrelated)
		})
	}
}
