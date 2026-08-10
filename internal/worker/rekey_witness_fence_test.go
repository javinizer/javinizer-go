package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openFailSuffixFS wedges Open for paths with the given suffix (afero.ReadFile
// and ReadDir both funnel through Open).
type openFailSuffixFS struct {
	afero.Fs
	suffix string
}

func (f openFailSuffixFS) Open(n string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(n), filepath.ToSlash(f.suffix)) {
		return nil, errors.New("open wedged")
	}
	return f.Fs.Open(n)
}

// codex P2: the promotion writer names witnesses with url.PathEscape
// (.promote-A%20B.json); the rekey fence must probe the SAME encoded name or
// an unresolved promotion on an ID containing spaces slips past the fence.
func TestRekeyBlockedByEscapedPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "AB 12", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-AB%2012.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "AB 12"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "AB 13"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness unresolved", "encoded promote witness must fence the rekey")
}

// codex P2 arbitration fence: a same-family WHOLE-MOVIE PATCH (no rekey)
// still advances the result revision; with an unresolved promote witness
// outstanding that revision bump would make the startup reconciler
// misdeclare the pending promotion committed. Refuse until reconciled.
func TestWholeMoviePatchFencedByPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-SSNI-R1.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-R1", Title: "renamed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness unresolved")
}

// Same fence via an outstanding CROP witness naming the poster.
func TestWholeMoviePatchFencedByCropWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-SSNI-R1.crop-1.json"), []byte("{\"poster_id\":\"SSNI-R1\"}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-R1", Title: "renamed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness unresolved")
}

// Field-level cherry-picks advance the revision too — same fence applies.
func TestApplyFieldOverrideFencedByPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-SSNI-R1.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	_, _, err := m.ApplyFieldOverride(context.Background(), "res-1", "title", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness unresolved")
}

func TestApplyFieldOverrideFencedByCropWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-SSNI-R1.crop-9.json"), []byte("{\"poster_id\":\"SSNI-R1\"}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	_, _, err := m.ApplyFieldOverride(context.Background(), "res-1", "title", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness unresolved")
}

// nil/zero env and empty poster id gracefully pass the fence.
func TestPosterWitnessFenceEnvEdges(t *testing.T) {
	assert.NoError(t, posterWitnessFence(nil, "SSNI-R1"))
	assert.NoError(t, posterWitnessFence(&posterEditEnv{}, "SSNI-R1"))
	assert.NoError(t, posterWitnessFence(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "J"}, ""))
}

// audit F7: the single-save surface (UpdateMovie → updateMovieSingleLocked)
// advances the revision like UpdateMovieFamily — it must be fenced too.
func TestUpdateMovieSingleFencedByPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-SSNI-R1.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.updateMovieSingleLocked(context.Background(), "/f/a.mp4", &models.Movie{ID: "SSNI-R1", Title: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness unresolved")
}

// The parked-marker probe inside the full witness fence fails closed.
func TestPosterWitnessConflictParkedScanErrorFailsClosed(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JXP"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	fs := &openFailAfterNFS{Fs: mem, suffix: dir, allow: 4} // promote stat + rekey scan ok; parked scan wedges
	err := posterWitnessConflict(fs, "/tmp", "JXP", "PI-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup-scan")
	fresh := &openFailAfterNFS{Fs: mem, suffix: dir, allow: 4} // fresh counter: same gating layout
	require.NoError(t, posterWitnessConflictCore(fresh, "/tmp", "JXP", "PI-1"), "core never reads the parked probe")
}

// audit F-R6-1: inbound fence matches NewID by content — an edit or rescrape
// resolving INTO a pending witness's destination is refused.
func TestFenceMatchesRekeyWitnessNewID(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-N"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-7.json"), []byte("{\"old_id\":\"OLD-7\",\"new_id\":\"NEW-7\"}"), 0o644))
	err := posterWitnessConflict(fs, "/tmp", "JOB-N", "NEW-7")
	require.Error(t, err, "NewID matched by content")
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	require.NoError(t, posterWitnessConflict(fs, "/tmp", "JOB-N", "UNRELATED-7"), "unrelated passes")
}

// audit F-R6-1: relocation refuses to move bytes into an identity that a
// pending witness names as its destination.
func TestRekeyDestinationFencedByPendingWitness(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	w, _ := json.Marshal(rekeyWitness{OldID: "OTHER-X", NewID: "SSNI-N9"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OTHER-X.json"), w, 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "destination")
	for _, name := range []string{"SSNI-R1-full.jpg", "SSNI-R1.jpg"} {
		_, statErr := fs.Stat(filepath.Join(dir, name))
		assert.NoError(t, statErr, "originals untouched on destination fence")
	}

	// fail-closed on scan wedges
	base2 := afero.NewMemMapFs()
	dir2 := filepath.Join("/tmp", "posters", "JOB-W")
	require.NoError(t, base2.MkdirAll(dir2, 0o755))
	require.NoError(t, afero.WriteFile(base2, filepath.Join(dir2, ".rekey-x.json"), []byte("{}"), 0o644))
	store2 := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store2, "/f/a.mp4", "res-1", "SSNI-R1", "")
	pe2 := newEditorForStore(store2)
	pe2.attachEnv(&posterEditEnv{fs: openFailSuffixFS{Fs: base2, suffix: ".rekey-x.json"}, tempDir: "/tmp", jobID: "JOB-W"})
	m2 := &LockedMovieOps{pe: pe2, movieID: "SSNI-R1"}
	err2 := m2.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "witness scan", "scan wedge ⇒ hard error")
}

// openFailAfterNFS wedges the FIRST n+1'th Open call on paths with the suffix
// — drives the crop scan's ReadDir error while the rekey scan sees the dir.
type openFailAfterNFS struct {
	afero.Fs
	suffix string
	allow  int
	count  int
}

func (f *openFailAfterNFS) Open(p string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(p), filepath.ToSlash(f.suffix)) {
		f.count++
		if f.count > f.allow {
			return nil, errors.New("open wedged")
		}
	}
	return f.Fs.Open(p)
}

func TestRekeyWitnessIDsEdges(t *testing.T) {
	fs := afero.NewMemMapFs()
	hit, err := rekeyWitnessIDsFor(fs, "/no/such/dir", "X-1")
	assert.False(t, hit)
	assert.NoError(t, err, "absent dir is not an error")

	dir := "/tmp/posters/JE"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-x.json"), []byte("{corrupt"), 0o644))
	hit, err = rekeyWitnessIDsFor(fs, dir, "X-1")
	assert.False(t, hit)
	assert.NoError(t, err, "corrupt payload skips to reconciler ownership")
}

func TestPosterWitnessFenceCropDirReadError(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JB"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	fs := &openFailAfterNFS{Fs: mem, suffix: dir, allow: 2} // folded promote scan #1, rekey scan #2; the crop scan (#3) wedges now
	err := posterWitnessFence(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JB"}, "PI-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

// audit F5+F-R6-1: the content-based rekey scan fails closed — a wedge on
// EITHER the directory listing or a witness file read must fence.
func TestPosterWitnessFenceRekeyStatErrorFailsClosed(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-9"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".rekey-SSNI-R1.json"), []byte("{\"old_id\":\"SSNI-R1\"}"), 0o644))
	fs := openFailSuffixFS{Fs: mem, suffix: ".rekey-SSNI-R1.json"}
	err := posterWitnessFence(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"}, "SSNI-R1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rekey witness check")
}

// codex P2 fail-closed: a directory LISTING error (not mere absence) also
// fails the scan closed.
func TestRekeyCropWitnessScanDirErrorFailsClosed(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	fs := openFailSuffixFS{Fs: base, suffix: "JOB-9"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "witness scan", "listing wedges fail the poster-witness scans closed")
}
