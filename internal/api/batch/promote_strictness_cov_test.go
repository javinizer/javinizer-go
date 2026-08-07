package batch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex r51 P2a: a transient destination Stat error must abort the promote
// rather than rename over an unseen existing file.
type statErrTargetFS struct {
	afero.Fs
	target string
}

func (f statErrTargetFS) Stat(name string) (os.FileInfo, error) {
	if name == f.target {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(name)
}

func TestPromoteStagedPosterPairAbortsOnTransientStatError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J9"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "M-1-full.jpg"), []byte("oldfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "M-1.stage-x-full.jpg"), []byte("newfull"), 0o644))
	fs := statErrTargetFS{Fs: base, target: filepath.Join(dir, "M-1-full.jpg")}

	_, err := promoteStagedPosterPair(fs, "/tmp", "J9", "M-1.stage-x", "M-1")
	require.ErrorContains(t, err, "promote target stat")
	canon, rerr := afero.ReadFile(base, filepath.Join(dir, "M-1-full.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "oldfull", string(canon), "existing bytes never destroyed")
	_, bakErr := base.Stat(filepath.Join(dir, "M-1-full.jpg.bak"))
	assert.Error(t, bakErr, "no partial .bak parking")
	_, stageErr := base.Stat(filepath.Join(dir, "M-1.stage-x-full.jpg"))
	assert.NoError(t, stageErr, "staged bytes remain for cleanup")
}

// codex r51 P2b: an outstanding witness must refuse a second promotion.
func TestWritePromoteWitnessGuardedRejectsUnresolved(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JG"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	first, err := writePromoteWitnessGuarded(fs, "/tmp", "JG", "PI-1", "https://x/old.jpg", "res-1", 0, nil)
	require.NoError(t, err)
	_, err = writePromoteWitnessGuarded(fs, "/tmp", "JG", "PI-1", "https://x/new.jpg", "res-1", 0, nil)
	require.ErrorIs(t, err, errPromoteWitnessPending)
	removePromoteWitness(fs, first)
	_, err = writePromoteWitnessGuarded(fs, "/tmp", "JG", "PI-1", "https://x/new.jpg", "res-1", 0, nil)
	require.NoError(t, err, "sweeping the witness readmits the operation")
}
