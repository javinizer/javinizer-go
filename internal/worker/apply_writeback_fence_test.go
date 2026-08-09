package worker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// witnessOpenFailFS faults reads of the named witness file — the probe's
// content-scan fail-closed seam (pitched after the codex cloud P1 fold).
type witnessOpenFailFS struct {
	afero.Fs
	suffix string
}

func (f witnessOpenFailFS) Open(name string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(name), f.suffix) {
		return nil, errors.New("open wedged")
	}
	return f.Fs.Open(name)
}

// codex P2: a transient Stat error on the witness probe must fence the
// write-back (return true), not admit it as "no witness".
func TestHasUnresolvedPromoteWitnessStatErrorFences(t *testing.T) {
	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll("/tmp/posters/JOB-9", 0o755))
	pe := newEditorForStore(resultstore.New(1, []string{"/f/a.mp4"}))
	// codex cloud P1 reseat: the folded content scan fails closed when the
	// witness DIRECTORY itself cannot be enumerated (no witness file needed).
	pe.attachEnv(&posterEditEnv{fs: witnessOpenFailFS{Fs: mem, suffix: "/tmp/posters/JOB-9"}, tempDir: "/tmp", jobID: "JOB-9"})
	assert.True(t, pe.hasUnresolvedPromoteWitness("PI-1"), "probe error => conservatively fenced")
}

// codex P2: apply FAILURE write-backs bump the revision too — fence them
// behind an unresolved promote witness just like the success path.
func TestInterpretApplyResultFailureSkipsWritebackWhenPromoteWitnessUnresolved(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-ff", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "OK-3", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-3"},
	})
	before, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	inputs := minimalApplyInputs(t, store, true)
	inputs.PromoteWitnessFn = func(posterID string) bool { return posterID == "OK-3" }
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-3"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OK-3"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, errors.New("apply engine wedged"))
	assert.True(t, outcome.Failed)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "failure write-back skipped — revision untouched")
	assert.Equal(t, models.JobStatusRunning, final.Status, "failure status write-back skipped")
}

// codex P2: apply panics are intercepted by withFileRecovery, NOT the
// interpret failure branch — this recovery write-back must fence behind an
// unresolved promote witness too, or its revision bump flips arbitration.
func TestRecoveryPanicSkipsWritebackWhenPromoteWitnessUnresolved(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-rc", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "PAN-9", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PAN-9"},
	})
	before, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		filePath:         "/f/a.mp4",
		fmi:              models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PAN-9"},
		movie:            &models.Movie{ID: "PAN-9"},
		updater:          store,
		promoteWitnessFn: func(id string) bool { return id == "PAN-9" },
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("boom")
	}()
	assert.True(t, outcome.Failed)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "panic recovery write-back fenced")
	assert.Equal(t, models.JobStatusRunning, final.Status)
}

// Fence identity falls back to the movie ID when the match carries none.
func TestRecoveryFenceFallsBackToMovieID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/b.mp4"})
	store.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-rf", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "PAN-10", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4"},
	})
	before, err := store.GetMovieResult("/f/b.mp4")
	require.NoError(t, err)
	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		filePath:         "/f/b.mp4",
		fmi:              models.FileMatchInfo{Path: "/f/b.mp4"},
		movie:            &models.Movie{ID: "PAN-10"},
		updater:          store,
		promoteWitnessFn: func(id string) bool { return id == "PAN-10" },
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("boom")
	}()
	final, err := store.GetMovieResult("/f/b.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "fenced via fallback movie-ID probe")
}

// audit R2: crop and rekey witnesses fence the write-back too (a crop's
// arbitration degenerates to revision-only once the row carries the URL).
func TestHasUnresolvedPromoteWitnessAllKinds(t *testing.T) {
	newPE := func(base afero.Fs) *PosterEditor {
		pe := newEditorForStore(resultstore.New(1, []string{"/f/a.mp4"}))
		pe.attachEnv(&posterEditEnv{fs: base, tempDir: "/tmp", jobID: "JOB-9"})
		return pe
	}

	crop1 := afero.NewMemMapFs()
	require.NoError(t, crop1.MkdirAll("/tmp/posters/JOB-9", 0o755))
	require.NoError(t, afero.WriteFile(crop1, "/tmp/posters/JOB-9/.crop-PI-1.crop-x.json", []byte("{\"poster_id\":\"PI-1\"}"), 0o644))
	assert.True(t, newPE(crop1).hasUnresolvedPromoteWitness("PI-1"), "crop witness fences")

	rekey1 := afero.NewMemMapFs()
	require.NoError(t, rekey1.MkdirAll("/tmp/posters/JOB-9", 0o755))
	require.NoError(t, afero.WriteFile(rekey1, "/tmp/posters/JOB-9/.rekey-PI-1.json", []byte("{\"old_id\":\"PI-1\"}"), 0o644))
	assert.True(t, newPE(rekey1).hasUnresolvedPromoteWitness("PI-1"), "rekey witness fences")
}

// audit R3: the fence probes alias AND canonical spellings — witnesses are
// named by the canonical movie ID.
func TestRecoveryFenceProbesCanonicalSpelling(t *testing.T) {
	store := resultstore.New(1, []string{"/f/c.mp4"})
	store.UpdateFileResult("/f/c.mp4", &resultstore.MovieResult{
		ResultID: "res-rc2", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "ABC-123", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "ABC123"},
	})
	before, err := store.GetMovieResult("/f/c.mp4")
	require.NoError(t, err)
	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		// alias only on the match surface; canonical on the movie
		filePath: "/f/c.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "ABC123"},
		movie:    &models.Movie{ID: "ABC-123"},
		updater:  store,
		promoteWitnessFn: func(id string) bool {
			return id == "ABC-123" // canonical-named witness only
		},
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("boom")
	}()
	final, err := store.GetMovieResult("/f/c.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "fenced via the canonical spelling despite alias-only match info")
}

// Fence-id dedupe: when the match alias and canonical movie ID are the same
// string, the witness probe runs exactly once.
func TestRecoveryFenceDedupesIdenticalSpellings(t *testing.T) {
	store := resultstore.New(1, []string{"/f/d.mp4"})
	store.UpdateFileResult("/f/d.mp4", &resultstore.MovieResult{
		ResultID: "res-dup", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "DUP-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/d.mp4", MovieID: "DUP-1"},
	})
	calls := 0
	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		filePath: "/f/d.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/d.mp4", MovieID: "DUP-1"},
		movie:    &models.Movie{ID: "DUP-1"},
		updater:  store,
		promoteWitnessFn: func(id string) bool {
			calls++
			return false
		},
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("boom")
	}()
	assert.Equal(t, 1, calls, "alias==canonical probes once")
}

func TestHasUnresolvedPromoteWitness(t *testing.T) {
	pe := newEditorForStore(resultstore.New(1, []string{"/f/a.mp4"}))
	assert.False(t, pe.hasUnresolvedPromoteWitness("PI-1"), "nil env")

	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	assert.False(t, pe.hasUnresolvedPromoteWitness("PI-1"), "no witness on disk")
	assert.False(t, pe.hasUnresolvedPromoteWitness(""), "empty poster id")

	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), []byte("{}"), 0o644))
	assert.True(t, pe.hasUnresolvedPromoteWitness("PI-1"))
}

// codex P2: the apply success write-back advances Revision — while a promote
// witness is unresolved that bump would flip startup arbitration of the
// pending promote to "committed". The write-back must be skipped.
func TestInterpretApplyResultSkipsWritebackWhenPromoteWitnessUnresolved(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-fwd", Status: models.JobStatusRunning, Revision: 5,
		Movie:         &models.Movie{ID: "OK-1", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"},
	})
	before, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	inputs := minimalApplyInputs(t, store, true)
	inputs.PromoteWitnessFn = func(posterID string) bool { return posterID == "OK-1" }
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"}}
	applyMovie := &models.Movie{ID: "OK-1", Title: "scraped"}
	result := &workflow.ApplyResult{Movie: &models.Movie{ID: "OK-1", Title: "scraped"}}
	outcome := interpretApplyResult("/f/a.mp4", applyMovie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, nil)
	assert.True(t, outcome.Success)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "write-back skipped — revision untouched so arbitration stays truthful")
	assert.Equal(t, "live", final.Movie.Title)
}
