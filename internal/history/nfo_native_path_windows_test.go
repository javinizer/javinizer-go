//go:build windows

package history

import (
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Map synthetic Windows volumes into temporary storage. Rooted volume-less
// paths select D:, so stripping C: cannot accidentally pass on a C: worker.
// No test reaches a physical C:, D:, or UNC location outside t.TempDir().
type nfoWindowsVolumeFS struct {
	afero.Fs
	root           string
	mkdirs, writes []string
}

func (f *nfoWindowsVolumeFS) mapped(p string) string {
	volume := filepath.VolumeName(p)
	tail := strings.TrimLeft(strings.TrimPrefix(p, volume), `\/`)
	if volume == "" {
		volume = "D:"
	}
	volume = strings.NewReplacer(":", "", `\`, "_", "/", "_").Replace(volume)
	return filepath.Join(f.root, volume, tail)
}
func (f *nfoWindowsVolumeFS) MkdirAll(p string, m os.FileMode) error {
	f.mkdirs = append(f.mkdirs, p)
	return f.Fs.MkdirAll(f.mapped(p), m)
}
func (f *nfoWindowsVolumeFS) OpenFile(p string, flag int, m os.FileMode) (afero.File, error) {
	f.writes = append(f.writes, p)
	return f.Fs.OpenFile(f.mapped(p), flag, m)
}
func (f *nfoWindowsVolumeFS) Open(p string) (afero.File, error) { return f.Fs.Open(f.mapped(p)) }

func TestNFONativeWindowsRestoreKeepsVolume(t *testing.T) {
	for _, hard := range []bool{false, true} {
		for _, dir := range []string{`C:\incoming`, `D:\incoming`, `\\synthetic-server\share\incoming`} {
			t.Run(dir+map[bool]string{false: "/soft", true: "/hard"}[hard], func(t *testing.T) {
				fs := &nfoWindowsVolumeFS{Fs: afero.NewOsFs(), root: t.TempDir()}
				intended := filepath.Join(dir, "movie.nfo")
				wrong := `D:\incoming\movie.nfo`
				if filepath.VolumeName(dir) == "D:" {
					wrong = `C:\incoming\movie.nfo`
				}
				for p, body := range map[string]string{intended: "current", wrong: "foreign"} {
					require.NoError(t, fs.MkdirAll(filepath.Dir(p), 0755))
					require.NoError(t, afero.WriteFile(fs, p, []byte(body), 0644))
				}
				fs.mkdirs = nil
				fs.writes = nil
				op := &models.BatchFileOperation{OriginalPath: filepath.Join(dir, "movie.mp4"), NFOPath: intended, NFOSnapshot: "snapshot"}
				warning, result := restoreNFOFS(t.Context(), fs, nil, op, hard)
				require.Empty(t, warning)
				require.Nil(t, result)
				require.Equal(t, []string{filepath.Clean(dir)}, fs.mkdirs)
				require.Equal(t, []string{intended}, fs.writes)
				body, err := afero.ReadFile(fs, intended)
				require.NoError(t, err)
				require.Equal(t, "snapshot", string(body))
				body, err = afero.ReadFile(fs, wrong)
				require.NoError(t, err)
				require.Equal(t, "foreign", string(body))
			})
		}
	}
}

func TestNFONativeWindowsLegacyVolumeLossRefused(t *testing.T) {
	for _, hard := range []bool{false, true} {
		for _, legacy := range []string{`\incoming\movie.nfo`, `/incoming/movie.nfo`} {
			for _, original := range []string{`C:\incoming\movie.mp4`, `\\synthetic-server\share\incoming\movie.mp4`} {
				t.Run(legacy+map[bool]string{false: "/soft", true: "/hard"}[hard], func(t *testing.T) {
					fs := &nfoWindowsVolumeFS{Fs: afero.NewOsFs(), root: t.TempDir()}
					repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
					repo.On("UpdateRevertStatus", mock.Anything, uint(44), models.RevertStatusFailed).Return(nil).Once()
					op := &models.BatchFileOperation{ID: 44, OriginalPath: original, NFOPath: legacy, NFOSnapshot: "saved"}
					warning, result := restoreNFOFS(t.Context(), fs, repo, op, hard)
					require.Empty(t, warning)
					require.NotNil(t, result)
					require.Equal(t, models.RevertReasonNFORestoreFailed, result.Reason)
					require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
					require.Empty(t, fs.mkdirs)
					require.Empty(t, fs.writes)
				})
			}
		}
	}
}
