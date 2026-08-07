package batch

import (
	"errors"
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

func TestWritePromoteGuardedStatError(t *testing.T) {
	fs := statErrTargetFS{Fs: afero.NewMemMapFs(), target: "/tmp/posters/JG/.promote-PI-1.json"}
	_, err := writePromoteWitnessGuarded(fs, "/tmp", "JG", "PI-1", "https://x", "res-1", 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness check")
}

func TestWritePromoteWitnessNilFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JN", 0o755))
	p, err := writePromoteWitness(fs, "/tmp", "JN", "PI-1", "https://x", "res-1", 0, nil)
	require.NoError(t, err)
	assert.Contains(t, p, ".promote-PI-1.json")
}

func TestRemovePromoteWitnessNilFs(t *testing.T) {
	removePromoteWitness(nil, "/nonexistent/path")
}

func TestWriteCropWitnessNilFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JC", 0o755))
	p, err := writeCropWitness(fs, "/tmp", "JC", cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-1", CroppedURL: "https://x"})
	require.NoError(t, err)
	assert.Contains(t, p, ".crop-stage-1.json")
}

func TestRemoveCropWitnessNilFs(t *testing.T) {
	removeCropWitness(nil, "/nonexistent/path")
}

func TestPromoteCroppedLegNoStagedFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JX", 0o755))
	err := promoteCroppedLeg(fs, "/tmp", "JX", "stage-x", "PI-1")
	require.NoError(t, err)
}

func TestPromoteCroppedLegStatError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tmp/posters/JY", 0o755))
	require.NoError(t, afero.WriteFile(base, "/tmp/posters/JY/stage-x.jpg", []byte("x"), 0o644))
	fs := statErrTargetFS{Fs: base, target: "/tmp/posters/JY/stage-x.jpg"}
	err := promoteCroppedLeg(fs, "/tmp", "JY", "stage-x", "PI-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source stat")
}

func TestPosterPairBackupRestoreUnreadable(t *testing.T) {
	b := &posterPairBackup{
		fs:          afero.NewMemMapFs(),
		fullPath:    "/nonexistent/full.jpg",
		fullExisted: false, fullUnreadable: true,
	}
	assert.False(t, b.restore())
}

func TestPromoteCroppedLegRenameError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tmp/posters/JZ", 0o755))
	require.NoError(t, afero.WriteFile(base, "/tmp/posters/JZ/stage-x.jpg", []byte("x"), 0o644))
	fs := noRenameBatchFs{Fs: base}
	err := promoteCroppedLeg(fs, "/tmp", "JZ", "stage-x", "PI-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop promote")
}

type noRenameBatchFs struct{ afero.Fs }

func (noRenameBatchFs) Rename(string, string) error { return errors.New("rename blocked") }
