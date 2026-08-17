package fsutil

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCreateExclusiveStagingFile_CoverageW7(t *testing.T) {
	t.Run("collision retries and creates exclusively", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/collision.dat"
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
		require.NoError(t, afero.WriteFile(fs, dest+".rstr.1", []byte("occupied"), 0o644))

		staged, file, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 1, 0o600)
		require.NoError(t, err)
		require.Equal(t, dest+".rstr.2", staged)
		require.NoError(t, file.Close())
		require.NoError(t, fs.Remove(staged))
	})

	t.Run("non-collision errors surface", func(t *testing.T) {
		fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		_, _, err := CreateExclusiveStagingFile(fs, "/out/dest", ".rstr", 1, 0o600)
		require.Error(t, err)
	})

	t.Run("bounded exhaustion surfaces", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/exhausted.dat"
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
		for i := 1; i <= exclusiveStagingAttempts; i++ {
			name := dest + ".rstr." + strconv.FormatUint(uint64(i), 16)
			require.NoError(t, afero.WriteFile(fs, name, []byte("occupied"), 0o644))
		}

		_, _, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 1, 0o600)
		require.ErrorContains(t, err, "exclusive staging names exhausted")
	})
}
