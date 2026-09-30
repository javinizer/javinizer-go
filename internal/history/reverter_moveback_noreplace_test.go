package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// codex P1 (PRRT_kwDORn9KaM6npnwr): the no-follow vacancy prepass and the
// move-back mutation are temporally separated — the preceding planned-delete
// hashes stream between them — so another process can recreate the original
// path inside the gap. The move-back executes through the atomic no-replace
// primitive (fsutil.MoveFileNoReplace — the organize-side precedent): the late
// occupant SUPPRESSES the recovery with both objects retained byte-intact,
// instead of a plain POSIX rename replacing and destroying it.

// moveBackLatePlantFS plants a foreign occupant the first time a pending
// delete's pinned payload is opened for hashing — deterministically modelling
// a file recreated after the vacancy prepass but before the move-back loop
// runs: the prepass probes OriginalPath vacant, this Open is exactly the
// "planned-delete hashes are streaming" window the finding flags, and the
// rename executes only afterwards.
type moveBackLatePlantFS struct {
	afero.Fs
	trigger string
	plant   string
	data    []byte
	planted bool
}

func (f *moveBackLatePlantFS) Open(name string) (afero.File, error) {
	if !f.planted && filepath.Clean(name) == filepath.Clean(f.trigger) {
		if err := afero.WriteFile(f.Fs, f.plant, f.data, 0o666); err != nil {
			return nil, err
		}
		f.planted = true
	}
	return f.Fs.Open(name)
}

func moveBackLateOp(src, dst, pinned string, pinBytes []byte) *models.BatchFileOperation {
	return &models.BatchFileOperation{
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			PlannedDeletes: []models.DeleteEntry{{Path: pinned, SHA256: sha256HexOf(pinBytes)}},
			MoveBack:       []models.FileMove{{OriginalPath: src, NewPath: dst}},
		}),
	}
}

func TestCleanupGeneratedFilesFS_MoveBackLateOccupantInProbeGapRetainedNoReplace(t *testing.T) {
	pinBytes := []byte("pinned subtitle bytes streaming through the hash leg")
	moved := []byte("moved sibling payload")
	occupant := []byte("foreign late occupant — recreated after the vacancy probe")

	t.Run("memfs", func(t *testing.T) {
		const (
			src    = "/src-w161n/W161N-001-cd2.mp4"
			dst    = "/dst-w161n/lib/W161N-001-cd2.mp4"
			pinned = "/dst-w161n/lib/W161N-001-cd2.srt"
		)
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, base.MkdirAll(filepath.Dir(src), 0o777))
		require.NoError(t, afero.WriteFile(base, dst, moved, 0o666))
		require.NoError(t, afero.WriteFile(base, pinned, pinBytes, 0o666))
		fs := &moveBackLatePlantFS{Fs: base, trigger: pinned, plant: src, data: occupant}

		cleanupGeneratedFilesFS(fs, moveBackLateOp(src, dst, pinned, pinBytes), "/dst-w161n")

		require.True(t, fs.planted, "the occupant genuinely landed inside the probe→rename gap")
		got, err := afero.ReadFile(base, src)
		require.NoError(t, err)
		assert.Equal(t, occupant, got, "the late occupant is never replaced by the move-back")
		got, err = afero.ReadFile(base, dst)
		require.NoError(t, err, "recovery suppressed — the moved sibling stays put alongside the occupant")
		assert.Equal(t, moved, got)
	})

	t.Run("osfs", func(t *testing.T) {
		base := afero.NewOsFs()
		root := t.TempDir()
		src := filepath.Join(root, "src", "W161N-001-cd2.mp4")
		dst := filepath.Join(root, "dst", "lib", "W161N-001-cd2.mp4")
		pinned := filepath.Join(root, "dst", "lib", "W161N-001-cd2.srt")
		require.NoError(t, base.MkdirAll(filepath.Dir(src), 0o777))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, afero.WriteFile(base, dst, moved, 0o666))
		require.NoError(t, afero.WriteFile(base, pinned, pinBytes, 0o666))
		fs := &moveBackLatePlantFS{Fs: base, trigger: pinned, plant: src, data: occupant}

		cleanupGeneratedFilesFS(fs, moveBackLateOp(src, dst, pinned, pinBytes), filepath.Join(root, "dst"))

		require.True(t, fs.planted, "the occupant genuinely landed inside the probe→rename gap")
		got, err := afero.ReadFile(base, src)
		require.NoError(t, err)
		assert.Equal(t, occupant, got, "the late occupant is never replaced by the move-back")
		got, err = afero.ReadFile(base, dst)
		require.NoError(t, err, "recovery suppressed — the moved sibling stays put alongside the occupant")
		assert.Equal(t, moved, got)
	})
}

// The unchanged-contract leg beside it: with the original path still vacant at
// the mutation instant the atomic no-replace move-back restores the consumed
// sibling exactly like the plain rename did (the pinned planned-delete keeps
// its stopAt bookkeeping intact).
func TestCleanupGeneratedFilesFS_MoveBackVacantOriginalStillRestoresNoReplace(t *testing.T) {
	const (
		src    = "/src-w161v/W161V-001-cd2.mp4"
		dst    = "/dst-w161v/lib/W161V-001-cd2.mp4"
		pinned = "/dst-w161v/lib/W161V-001-cd2.srt"
	)
	pinBytes := []byte("pinned subtitle bytes")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o777))
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o777))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("part two"), 0o666))
	require.NoError(t, afero.WriteFile(fs, pinned, pinBytes, 0o666))

	cleanupGeneratedFilesFS(fs, moveBackLateOp(src, dst, pinned, pinBytes), "/dst-w161v")

	got, err := afero.ReadFile(fs, src)
	require.NoError(t, err, "the vacant original path gets the sibling restored")
	assert.Equal(t, "part two", string(got))
	if _, err := fs.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("the consumed move vacated the destination: %v", err)
	}
}
