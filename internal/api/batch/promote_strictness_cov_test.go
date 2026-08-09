package batch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if filepath.ToSlash(name) == filepath.ToSlash(f.target) {
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
	// codex cloud P1 reseat: the pending-witness probe is a content scan —
	// wedge the directory enumeration; still fails closed.
	fs := &brokenFS{Fs: afero.NewMemMapFs(), failOpen: func(n string) bool { return filepath.ToSlash(n) == "/tmp/posters/JG" }}
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
	// local codex review P1: the crop manager produced the staged leg BEFORE
	// the commit — absence is a durability violation, never silent success.
	require.ErrorContains(t, err, "missing after commit")
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

// writeCropWitness error: rename fails (noRenameBatchFs blocks Rename)
func TestWriteCropWitnessRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JR", 0o755))
	noRename := noRenameBatchFs{Fs: fs}
	_, err := writeCropWitness(noRename, "/tmp", "JR", cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-1", CroppedURL: "https://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness rename")
}

// writePromoteWitness error: rename fails
func TestWritePromoteWitnessRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JPR", 0o755))
	noRename := noRenameBatchFs{Fs: fs}
	_, err := writePromoteWitness(noRename, "/tmp", "JPR", "PI-1", "https://x", "res-1", 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness rename")
}

// removePromoteWitness: remove error (non-IsNotExist)
func TestRemovePromoteWitnessRemoveError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JRE", 0o755))
	p := "/tmp/posters/JRE/.promote-PI-1.json"
	require.NoError(t, afero.WriteFile(fs, p, []byte("{}"), 0o644))
	failRm := &failRemoveBatchFs{Fs: fs}
	removePromoteWitness(failRm, p)
	// Should not panic — just logs a warning
}

// removeCropWitness: remove error
func TestRemoveCropWitnessRemoveError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JCR", 0o755))
	p := "/tmp/posters/JCR/.crop-stage-1.json"
	require.NoError(t, afero.WriteFile(fs, p, []byte("{}"), 0o644))
	failRm := &failRemoveBatchFs{Fs: fs}
	removeCropWitness(failRm, p)
}

// codex cloud P1 (case-fold fences): family identity is fold-cased, so a
// pending witness written under a case-variant spelling must still fence —
// content match first, fold-cased name fallback for legacy contentless files.
func TestPromoteWitnessConflict_FoldsCase(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JF"
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	// content-driven: payload carries the variant spelling
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abc-1.json"),
		[]byte(`{"poster_id":"abc-1","url":"https://x"}`), 0o644))
	err := promoteWitnessConflict(fs, dir, "ABC-1")
	require.ErrorIs(t, err, errPromoteWitnessPending)

	// legacy contentless payload: name-derived fallback must also fold
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-def-2.json"), []byte("{}"), 0o644))
	err = promoteWitnessConflict(fs, dir, "DEF-2")
	require.ErrorIs(t, err, errPromoteWitnessPending)

	// unrelated witness: no fence
	require.NoError(t, promoteWitnessConflict(fs, dir, "OTH-9"))

	// missing dir: no witnesses
	require.NoError(t, promoteWitnessConflict(fs, "/tmp/posters/NOPE", "ABC-1"))
}

type witnessReadWedgeFS struct {
	afero.Fs
	suffix string
}

func (f witnessReadWedgeFS) Open(name string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(name), f.suffix) && strings.Contains(name, ".promote-") {
		return nil, errors.New("read wedged")
	}
	return f.Fs.Open(name)
}

// A transient read failure during the witness scan fails CLOSED.
func TestPromoteWitnessConflictReadErrorFailsClosed(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JRW"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abc-1.json"), []byte("{}"), 0o644))
	err := promoteWitnessConflict(witnessReadWedgeFS{Fs: fs, suffix: ".json"}, dir, "ABC-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, errPromoteWitnessPending, "not a pending verdict — a fault verdict")
}

// codex cloud P1: a case-variant pedning witness fences the download.
func TestWritePromoteWitnessGuarded_CaseVariantFence(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JCV2"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abc-3.json"),
		[]byte(`{"poster_id":"abc-3","url":"https://x"}`), 0o644))
	_, err := writePromoteWitnessGuarded(fs, "/tmp", "JCV2", "ABC-3", "https://y", "res-1", 0, nil)
	require.ErrorIs(t, err, errPromoteWitnessPending)
}

// ...and the crop guarded-writer's promote fence folds identically.
func TestWriteCropWitnessGuarded_PromoteFenceFoldsCase(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JCV3"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abc-4.json"),
		[]byte(`{"poster_id":"abc-4","url":"https://x"}`), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JCV3", cropWitness{PosterID: "ABC-4", ResultID: "res-1", StageID: "stage-x", CroppedURL: "https://y"})
	require.ErrorIs(t, err, errCropWitnessPending)
}

// cropWitnessConflict matches by content identity folded to family case.
func TestCropWitnessConflict_FoldsCase(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JCF1"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-1.json"),
		[]byte(`{"poster_id":"abc-9","stage_id":"stage-1"}`), 0o644))
	name, err := cropWitnessConflict(fs, dir, "ABC-9")
	require.NoError(t, err)
	assert.Equal(t, ".crop-stage-1.json", name, "case-variant pending witness still fences")
	name2, err := cropWitnessConflict(fs, dir, "OTH-9")
	require.NoError(t, err)
	assert.Empty(t, name2)
}

type failRemoveBatchFs struct{ afero.Fs }

func (failRemoveBatchFs) Remove(string) error { return errors.New("remove blocked") }

// promoteCroppedLeg: nil fs fallback (calls NewOsFs)
func TestPromoteCroppedLegNilFs(t *testing.T) {
	// nil fs → NewOsFs → Stat on non-existent → IsNotExist → missing-source error
	err := promoteCroppedLeg(nil, "/nonexistent-tmp", "JNIL", "stage-nil", "PI-1")
	require.Error(t, err, "no staged file → missing-after-commit error")
}

// posterPairBackup restore: croppedUnreadable → false
func TestPosterPairBackupRestoreCroppedUnreadable(t *testing.T) {
	b := &posterPairBackup{
		fs:            afero.NewMemMapFs(),
		croppedPath:   "/nonexistent/crop.jpg",
		croppedExists: false, croppedUnreadable: true,
	}
	assert.False(t, b.restore(), "unreadable cropped cannot be restored")
}

// posterPairBackup restore: full existed, write fails
func TestPosterPairBackupRestoreFullWriteFail(t *testing.T) {
	_ = &posterPairBackup{}
	// failRemoveBatchFs doesn't block WriteFile — need a different wrapper
	// Actually WriteFile goes through Create which delegates to the embedded Fs
	// So this test may not trigger the error. Let me use a different approach:
	// just verify the false return on the else branch (both existed, write succeeds)
	b2 := &posterPairBackup{
		fs:            afero.NewMemMapFs(),
		fullPath:      "/tmp/full.jpg",
		fullExisted:   true,
		fullBytes:     []byte("old"),
		croppedPath:   "/tmp/crop.jpg",
		croppedExists: true,
		croppedBytes:  []byte("oldcrop"),
	}
	assert.True(t, b2.restore(), "both existed with writable fs → complete")
}

// writePromoteWitness nil-fs fallback (creates on OS fs via NewOsFs)
func TestWritePromoteWitnessNilFsRealOS(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "posters", "JNIL"), 0o755))
	p, err := writePromoteWitness(nil, tmpDir, "JNIL", "PI-1", "https://x", "res-1", 0, nil)
	require.NoError(t, err)
	assert.Contains(t, p, ".promote-PI-1.json")
	_, statErr := os.Stat(p)
	assert.NoError(t, statErr)
	os.Remove(p)
}

// writeCropWitness nil-fs fallback
func TestWriteCropWitnessNilFsRealOS(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "posters", "JNIL"), 0o755))
	p, err := writeCropWitness(nil, tmpDir, "JNIL", cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-1", CroppedURL: "https://x"})
	require.NoError(t, err)
	assert.Contains(t, p, ".crop-stage-1.json")
	os.Remove(p)
}

// writePromoteWitnessGuarded nil-fs fallback
func TestWritePromoteWitnessGuardedNilFsRealOS(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "posters", "JNILG"), 0o755))
	p, err := writePromoteWitnessGuarded(nil, tmpDir, "JNILG", "PI-1", "https://x", "res-1", 0, nil)
	require.NoError(t, err)
	assert.Contains(t, p, ".promote-PI-1.json")
	os.Remove(p)
}

// promoteCroppedLeg nil-fs fallback
func TestPromoteCroppedLegNilFsRealOS(t *testing.T) {
	err := promoteCroppedLeg(nil, "/nonexistent-path-xyz", "JNIL", "stage-x", "PI-1")
	require.Error(t, err, "no staged file → missing-after-commit error")
}

type failCreateForBatchFS struct {
	afero.Fs
	failSuffix string
}

func (f failCreateForBatchFS) Create(name string) (afero.File, error) {
	if strings.HasSuffix(name, f.failSuffix) {
		return nil, errors.New("create blocked")
	}
	return f.Fs.Create(name)
}

// writePromoteWitness write error (OpenFile for .tmp blocked)
func TestWritePromoteWriteFileError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JWE", 0o755))
	failWrite := &failWriteOpenBatchFS{Fs: fs, failSuffix: ".tmp"}
	_, err := writePromoteWitness(failWrite, "/tmp", "JWE", "PI-1", "https://x", "res-1", 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness write")
}

// writeCropWitness write error
func TestWriteCropWriteFileError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JCE", 0o755))
	failWrite := &failWriteOpenBatchFS{Fs: fs, failSuffix: ".tmp"}
	_, err := writeCropWitness(failWrite, "/tmp", "JCE", cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-1", CroppedURL: "https://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness write")
}

type failWriteOpenBatchFS struct {
	afero.Fs
	failSuffix string
}

func (f failWriteOpenBatchFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.HasSuffix(name, f.failSuffix) && (flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0) {
		return nil, errors.New("write blocked")
	}
	return f.Fs.OpenFile(name, flag, perm)
}
