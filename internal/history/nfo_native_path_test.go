package history

import (
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type nfoRestoreMutationProbe struct {
	afero.Fs
	mkdirs, writes []string
}

func (f *nfoRestoreMutationProbe) MkdirAll(p string, m os.FileMode) error {
	f.mkdirs = append(f.mkdirs, p)
	return f.Fs.MkdirAll(p, m)
}
func (f *nfoRestoreMutationProbe) OpenFile(p string, flag int, m os.FileMode) (afero.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
		f.writes = append(f.writes, p)
	}
	return f.Fs.OpenFile(p, flag, m)
}

func TestNFONativeRestoreAbsoluteAndVirtualCompatibility(t *testing.T) {
	for _, hard := range []bool{false, true} {
		for _, dir := range []string{t.TempDir(), "relative-nfo", filepath.Join(string(filepath.Separator), "virtual-nfo")} {
			t.Run(dir+map[bool]string{false: "/soft", true: "/hard"}[hard], func(t *testing.T) {
				fs := &nfoRestoreMutationProbe{Fs: afero.NewMemMapFs()}
				nfoPath := filepath.Join(dir, "movie.nfo")
				op := &models.BatchFileOperation{OriginalPath: filepath.Join(dir, "movie.mp4"), NFOPath: nfoPath, NFOSnapshot: "saved"}
				warning, result := restoreNFOFS(t.Context(), fs, nil, op, hard)
				require.Empty(t, warning)
				require.Nil(t, result)
				wantDir, err := fsutil.CanonicalizePath(dir)
				require.NoError(t, err)
				want := fsutil.NormalizePath(filepath.Join(wantDir, "movie.nfo"))
				if filepath.IsAbs(dir) {
					want = filepath.Join(filepath.Clean(dir), "movie.nfo")
				}
				require.Equal(t, []string{want}, fs.writes)
				data, err := afero.ReadFile(fs, want)
				require.NoError(t, err)
				require.Equal(t, "saved", string(data))
			})
		}
	}
}

func TestNFONativeRestoreRelativeBasenameCompatibility(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := t.TempDir()
	op := &models.BatchFileOperation{OriginalPath: filepath.Join(dir, "movie.mp4"), NFOPath: "movie.nfo", NFOSnapshot: "saved"}
	warning, result := restoreNFOFS(t.Context(), fs, nil, op, false)
	require.Empty(t, warning)
	require.Nil(t, result)
	data, err := afero.ReadFile(fs, filepath.Join(dir, "movie.nfo"))
	require.NoError(t, err)
	require.Equal(t, "saved", string(data))
}

func TestNFONativeRestoreDifferentDirectories(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(map[bool]string{false: "soft", true: "hard"}[hard], func(t *testing.T) {
			fs := &nfoRestoreMutationProbe{Fs: afero.NewMemMapFs()}
			root := t.TempDir()
			originalDir := filepath.Join(root, "original")
			nfoDir := filepath.Join(root, "snapshot")
			op := &models.BatchFileOperation{OriginalPath: filepath.Join(originalDir, "movie.mp4"), NFOPath: filepath.Join(nfoDir, "custom.nfo"), NFOSnapshot: "saved"}
			warning, result := restoreNFOFS(t.Context(), fs, nil, op, hard)
			require.Empty(t, warning)
			require.Nil(t, result)
			wantDir := originalDir
			if hard {
				wantDir = nfoDir
			}
			require.Equal(t, []string{wantDir}, fs.mkdirs)
			require.Equal(t, []string{filepath.Join(wantDir, "custom.nfo")}, fs.writes)
		})
	}
}
