package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPublishNativeDirectoryNoReplace(t *testing.T) {
	for _, occupant := range []string{"absent", "file", "directory"} {
		t.Run(occupant, func(t *testing.T) {
			fs := afero.NewOsFs()
			parent := t.TempDir()
			source, target := filepath.Join(parent, "source"), filepath.Join(parent, "target")
			require.NoError(t, fs.MkdirAll(filepath.Join(source, "nested"), 0o755))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(source, "nested", "keep.txt"), []byte("source content"), 0o644))
			before, err := fs.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, before))
			if occupant == "file" {
				require.NoError(t, afero.WriteFile(fs, target, []byte("foreign content"), 0o644))
			} else if occupant == "directory" {
				require.NoError(t, fs.MkdirAll(target, 0o755))
				require.NoError(t, afero.WriteFile(fs, filepath.Join(target, "keep.txt"), []byte("foreign content"), 0o644))
			}
			err = PublishNoReplace(fs, source, target)
			if occupant == "absent" {
				require.NoError(t, err)
				require.NoDirExists(t, source)
				after, err := fs.Stat(target)
				require.NoError(t, err)
				require.True(t, os.SameFile(before, after))
				require.FileExists(t, filepath.Join(target, "nested", "keep.txt"))
			} else {
				require.Error(t, err)
				require.FileExists(t, filepath.Join(source, "nested", "keep.txt"))
				foreign := target
				if occupant == "directory" {
					foreign = filepath.Join(target, "keep.txt")
				}
				body, readErr := afero.ReadFile(fs, foreign)
				require.NoError(t, readErr)
				require.Equal(t, "foreign content", string(body))
			}
		})
	}
}
