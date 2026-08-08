package batch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 fence: with no poster directory at all the guard must not block a
// first crop witness.
func TestWriteCropWitnessGuardedWritesWhenDirMissing(t *testing.T) {
	fs := afero.NewMemMapFs()
	p, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-N", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.NoError(t, err)
	assert.Contains(t, filepath.ToSlash(p), ".crop-PI-1.crop-1.json")
}

// Unrelated neighbors (non-witness files, promote witnesses, crop witnesses
// for OTHER posters) must never fence this poster's crop.
func TestWriteCropWitnessGuardedSkipsUnrelatedEntries(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-U"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "note.txt"), []byte("x"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-OTHER-2.json"), []byte("{}"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OTHER-3.json"), []byte("{}"), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-U", cropWitness{PosterID: "OTHER-1", ResultID: "r1", StageID: "OTHER-1.crop-1"})
	require.NoError(t, err, "seeding an other-poster crop witness")
	_, err = writeCropWitnessGuarded(fs, "/tmp", "JOB-U", cropWitness{PosterID: "PI-1", ResultID: "r2", StageID: "PI-1.crop-2"})
	require.NoError(t, err, "unrelated entries must not fence")
	_, statErr := fs.Stat(filepath.Join(dir, ".crop-OTHER-1.crop-1.json"))
	assert.NoError(t, statErr, "foreign witness untouched")
}

// An outstanding witness for the SAME poster fences the new crop with 409
// semantics until the witness is reconciled.
func TestWriteCropWitnessGuardedFencesSamePoster(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-F"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	first, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-F", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.NoError(t, err)
	_, err = writeCropWitnessGuarded(fs, "/tmp", "JOB-F", cropWitness{PosterID: "PI-1", ResultID: "r2", StageID: "PI-1.crop-2"})
	require.ErrorIs(t, err, errCropWitnessPending)
	removeCropWitness(fs, first)
	_, err = writeCropWitnessGuarded(fs, "/tmp", "JOB-F", cropWitness{PosterID: "PI-1", ResultID: "r2", StageID: "PI-1.crop-2"})
	require.NoError(t, err, "sweeping the witness readmits the crop")
}

// Corrupt witness payloads belong to the startup reconciler — the guard
// skips them rather than blocking on bytes it cannot arbitrate.
func TestWriteCropWitnessGuardedSkipsCorruptWitness(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-C"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-PI-1.crop-x.json"), []byte("{nope"), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-C", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-9"})
	require.NoError(t, err)
}

// A transient read error mid-scan fails CLOSED — a half-counted fence is
// worse than a rejected crop.
func TestWriteCropWitnessGuardedScanReadFileFails(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-R"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".crop-PI-1.crop-x.json"), []byte("{}"), 0o644))
	fs := &brokenFS{Fs: mem, failOpen: func(n string) bool { return strings.HasSuffix(n, ".json") }}
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-R", cropWitness{PosterID: "PI-2", ResultID: "r1", StageID: "PI-2.crop-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

// A transient directory-listing error also fails closed.
func TestWriteCropWitnessGuardedReadDirFails(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-D"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	fs := &brokenFS{Fs: mem, failOpen: func(n string) bool { return filepath.ToSlash(n) == "/tmp/posters/JOB-D" }}
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-D", cropWitness{PosterID: "PI-2", ResultID: "r1", StageID: "PI-2.crop-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

func TestWriteCropWitnessGuardedNilFs(t *testing.T) {
	_, err := writeCropWitnessGuarded(nil, "/nonexistent-tmp", "JOB-Z", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	assert.Error(t, err, "osfs with a missing dir fails at the write stage")
}

func TestPromoteCroppedLegWithRetryFirstSuccess(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-P"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "stage-1.jpg"), []byte("fresh"), 0o644))
	require.NoError(t, promoteCroppedLegWithRetry(fs, "/tmp", "JOB-P", "stage-1", "PI-1"))
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "fresh", string(got))
	_, statErr := fs.Stat(filepath.Join(dir, "stage-1.jpg"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "stage consumed")
}

func TestPromoteCroppedLegWithRetryTransientThenSuccess(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-T"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "stage-1.jpg"), []byte("fresh"), 0o644))
	fs := &brokenFS{Fs: mem, failRenameAt: map[int]bool{1: true, 2: true}}
	require.NoError(t, promoteCroppedLegWithRetry(fs, "/tmp", "JOB-T", "stage-1", "PI-1"), "third attempt promotes")
	assert.Equal(t, 3, fs.renameCalls)
	got, rerr := afero.ReadFile(mem, filepath.Join(dir, "PI-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "fresh", string(got))
}

func TestPromoteCroppedLegWithRetryPersistentFailure(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-X"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "stage-1.jpg"), []byte("fresh"), 0o644))
	fs := &brokenFS{Fs: mem, failRenameAt: map[int]bool{1: true, 2: true, 3: true}}
	err := promoteCroppedLegWithRetry(fs, "/tmp", "JOB-X", "stage-1", "PI-1")
	require.ErrorContains(t, err, "crop promote")
	assert.Equal(t, 3, fs.renameCalls, "bounded at cropPromoteMaxAttempts")
	_, statErr := mem.Stat(filepath.Join(dir, "stage-1.jpg"))
	assert.NoError(t, statErr, "staged bytes survive for the reconciler")
}

// Handler level: an outstanding crop witness for the same poster fences the
// crop endpoint with a 409 until the witness is reconciled.
func TestPosterCrop_FencedByOutstandingCropWitness(t *testing.T) {
	_, job, router := cropJobFixture(t, "FENCE-001")
	witnessDir := filepath.Join("data/temp/posters", job.GetID())
	require.NoError(t, os.MkdirAll(witnessDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(witnessDir, ".crop-FENCE-001.crop-old.json"),
		[]byte(`{"poster_id":"FENCE-001","result_id":"res","stage_id":"FENCE-001.crop-old","cropped_url":"x","prev_revision":0}`), 0o644))

	w := postCrop(t, router, job, "FENCE-001", contracts.PosterCropRequest{X: 10, Y: 10, Width: 200, Height: 200})
	require.Equal(t, 409, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "crop witness")
}

// codex P2: an unresolved PROMOTE witness for this poster also fences a new
// crop (recreating bytes beside an in-flight promotion corrupts arbitration).
func TestWriteCropWitnessGuardedFencesPromoteWitness(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-PM"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), []byte("{}"), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-PM", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.ErrorIs(t, err, errCropWitnessPending)
}

// codex P2: an unresolved REKEY witness (.rekey-OLD.json — the worker writer
// names it with the raw ID) fences a new crop for that poster.
func TestWriteCropWitnessGuardedFencesRekeyWitness(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-RK"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-PI-1.json"), []byte("{}"), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-RK", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.ErrorIs(t, err, errCropWitnessPending)
}

// Witness stat probes fail CLOSED: a transient stat error is not absence.
func TestWriteCropWitnessGuardedPromoteStatErrorFailsClosed(t *testing.T) {
	fs := statErrTargetFS{Fs: afero.NewMemMapFs(), target: "/tmp/posters/JOB-PS/.promote-PI-1.json"}
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-PS", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

func TestWriteCropWitnessGuardedRekeyStatErrorFailsClosed(t *testing.T) {
	fs := statErrTargetFS{Fs: afero.NewMemMapFs(), target: "/tmp/posters/JOB-RS/.rekey-PI-1.json"}
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JOB-RS", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

// codex P2: a NON-absence read error on the canonical full-size source must
// abort the crop (409) rather than silently falling back to the cropped leg
// while the UI measured coordinates against full size.
func TestPosterCrop_StagingSourceReadError(t *testing.T) {
	deps, job, router := cropJobFixture(t, "RDERR-1")
	deps.Fs = &brokenFS{Fs: deps.GetFs(), failOpen: func(n string) bool {
		return strings.HasSuffix(n, "-full.jpg")
	}}
	w := postCrop(t, router, job, "RDERR-1", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	require.Equal(t, 409, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "staging source")
}
